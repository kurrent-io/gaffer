// The history timeline's model, ported from the CLI (cli/cmd/history.go
// collapseHistory + cli/cmd/history_graph.go computeHistoryGraph). Kept pure and
// render-independent - like the Go original - so the webview draws the same
// grammar the terminal does: run-state per node, revert brackets as lanes, a
// recreate's bookends folded into one row. The SVG rendering derives its
// geometry from this model (panels/history-view.ts); the model itself is unit
// tested against the same cases as the Go.

import type { HistoryEntry } from "../commands/history-schema.js";

// A collapsed row: a stream write, plus any bookend writes folded into it.
export interface CollapsedEntry extends HistoryEntry {
	absorbed: HistoryEntry[];
}

// collapseHistory folds a recreate's bookend writes into its stamped recreate
// row for the human timeline: the delete tombstone directly below it, then the
// disable (or a rewritten no-op) directly below that. Recreate's steps write
// consecutively, so only the exact adjacent pattern folds; anything interleaved
// stays a visible row. `history --json` is uncollapsed, so the viewer folds here.
export function collapseHistory(versions: HistoryEntry[]): CollapsedEntry[] {
	const out: CollapsedEntry[] = [];
	for (let i = 0; i < versions.length; i++) {
		const cur = versions[i];
		if (!cur) continue;
		const hv: CollapsedEntry = { ...cur, absorbed: [] };
		const next = versions[i + 1];
		if (hv.kind === "recreate" && next && next.kind === "deleted") {
			hv.absorbed.push(next);
			i++;
			const after = versions[i + 1];
			if (after && (after.kind === "disabled" || after.kind === "rewritten")) {
				hv.absorbed.push(after);
				i++;
			}
		}
		out.push(hv);
	}
	return out;
}

// historyMaxLanes bounds the timeline's width: the main line plus two branch
// lanes, so a revert nested inside a revert still draws, but nothing deeper does
// (it falls back to a flat row).
export const HISTORY_MAX_LANES = 3;

// A revert bracket: the content at row `top` (newer) reappears, matching the same
// content at row `bottom` (older). The rows strictly between are the detour that
// was undone, drawn in `lane` (1 = the first branch lane; the main line is 0).
export interface GraphSpan {
	top: number;
	bottom: number;
	lane: number;
}

// The lane layout for a timeline: the lane each row's node sits in and the revert
// brackets that were drawn. Derived purely from content-hash reappearance.
export interface HistoryGraph {
	nodeLane: number[];
	spans: GraphSpan[];
}

// A node the graph is laid out over: only content identity and whether the write
// was a state change matter. `CollapsedEntry` satisfies this.
export interface GraphNode {
	contentHash: string;
	stateChange?: boolean;
}

function sameSpan(a: GraphSpan, s: GraphSpan): boolean {
	return a.top === s.top && a.bottom === s.bottom && a.lane === s.lane;
}

// contains reports whether a fully encloses s. A shared endpoint still counts as
// containment; an identical span does not contain itself.
function contains(a: GraphSpan, s: GraphSpan): boolean {
	return a.top <= s.top && a.bottom >= s.bottom && !sameSpan(a, s);
}

// interleaves reports whether a and s overlap without either containing the other
// - the crossing we can't draw in stacked lanes. Touching at a single shared
// endpoint (sequential reverts) is not an interleave.
function interleaves(a: GraphSpan, s: GraphSpan): boolean {
	let [lo, hi] = [a, s];
	if (hi.top < lo.top) [lo, hi] = [hi, lo];
	return hi.top > lo.top && hi.top < lo.bottom && hi.bottom > lo.bottom;
}

// candidateSpans pairs each content hash with its previous occurrence, yielding a
// bracket wherever the same content reappears with a genuine detour between. Only
// content versions anchor a span - a state change carries no content identity, so
// it can't be an endpoint. The gap is counted in content versions, not raw rows.
// Spans come out sorted newest-top first, the order the greedy pass needs.
function candidateSpans(versions: readonly GraphNode[]): GraphSpan[] {
	const last = new Map<string, { index: number; pos: number }>();
	const spans: GraphSpan[] = [];
	let pos = 0;
	for (const [i, hv] of versions.entries()) {
		if (hv.contentHash === "" || hv.stateChange) continue;
		const o = last.get(hv.contentHash);
		if (o !== undefined && pos - o.pos > 1) {
			spans.push({ top: o.index, bottom: i, lane: 0 });
		}
		last.set(hv.contentHash, { index: i, pos });
		pos++;
	}
	spans.sort((a, b) => a.top - b.top);
	return spans;
}

// spanLane places a candidate relative to the already-accepted brackets: its lane
// is one past the deepest accepted span that contains it. Rejected (-1) if it
// interleaves with any accepted span (the accepted one has the newer top, so it
// wins) or if nesting it would exceed the lane cap.
function spanLane(s: GraphSpan, accepted: readonly GraphSpan[]): number {
	let depth = 0;
	for (const a of accepted) {
		if (contains(a, s)) depth++;
		else if (interleaves(a, s)) return -1;
	}
	const lane = depth + 1;
	return lane < HISTORY_MAX_LANES ? lane : -1;
}

// nodeLaneAt is the lane a row's node sits in: the deepest accepted span it's
// strictly interior to - a bracket's own endpoints stay on the enclosing line.
function nodeLaneAt(i: number, spans: readonly GraphSpan[]): number {
	let lane = 0;
	for (const s of spans) {
		if (s.top < i && i < s.bottom && s.lane > lane) lane = s.lane;
	}
	return lane;
}

// computeHistoryGraph lays out the revert brackets for a classified, newest-first
// history. It finds every content that reappears, then greedily accepts brackets
// newest-first: a bracket is dropped when it would cross an already-accepted one
// (newest wins) or nest deeper than the lane cap. Each row's node lane follows
// from the deepest bracket enclosing it.
export function computeHistoryGraph(
	versions: readonly GraphNode[],
): HistoryGraph {
	const spans: GraphSpan[] = [];
	for (const c of candidateSpans(versions)) {
		const lane = spanLane(c, spans);
		if (lane < 0) continue;
		spans.push({ top: c.top, bottom: c.bottom, lane });
	}
	const nodeLane = versions.map((_, i) => nodeLaneAt(i, spans));
	return { nodeLane, spans };
}
