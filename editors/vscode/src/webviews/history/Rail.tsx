import {
	createEffect,
	createMemo,
	createSignal,
	For,
	on,
	onCleanup,
	onMount,
} from "solid-js";
import type { RunState } from "../shared/StatusDot";
import { runState } from "./model";
import type { HistoryEntry, HistoryGraph } from "./protocol";
import styles from "./Rail.module.css";

// Geometry constants, matching the CLI's history_graph.go and the old inline
// renderer: lane spacing, node radius, spine weight, corner radius, step inset.
const PAD_L = 12;
const LANE_GAP = 20;
const NODE_R = 4;
const SPINE_W = 1.6;
const CORNER_R = 6;
const STEP = 14;

const RUN_VAR: Record<RunState, string> = {
	enabled: "var(--gx-enabled)",
	disabled: "var(--gx-disabled)",
	deleted: "var(--gx-deleted)",
};

const laneX = (lane: number) => PAD_L + lane * LANE_GAP;

// The rail gutter's width, from the deepest lane. Exposed so the rows can
// reserve it without re-deriving the geometry.
export function railWidth(graph: HistoryGraph): number {
	const maxLane = graph.nodeLane.reduce((m, l) => Math.max(m, l), 0);
	return PAD_L + maxLane * LANE_GAP + 12;
}

// An elbow between two lanes: down from (xa,ya), a rounded turn onto the
// horizontal at horizY, across, another turn, then down to (xb,yb).
function stepPath(
	xa: number,
	ya: number,
	xb: number,
	yb: number,
	horizY: number,
): string {
	const dir = xb > xa ? 1 : -1;
	return [
		`M ${xa} ${ya}`,
		`L ${xa} ${horizY - CORNER_R}`,
		`Q ${xa} ${horizY} ${xa + dir * CORNER_R} ${horizY}`,
		`L ${xb - dir * CORNER_R} ${horizY}`,
		`Q ${xb} ${horizY} ${xb} ${horizY + CORNER_R}`,
		`L ${xb} ${yb}`,
	].join(" ");
}

type Prim =
	| {
			type: "line";
			x1: number;
			y1: number;
			x2: number;
			y2: number;
			stroke: string;
			w: number;
			dash?: string;
			faded?: boolean;
	  }
	| { type: "path"; d: string; stroke: string; w: number }
	| { type: "dot"; cx: number; cy: number; fill: string }
	| { type: "ring"; cx: number; cy: number; stroke: string };

export function Rail(props: {
	entries: HistoryEntry[];
	graph: HistoryGraph;
	measureRoot: () => HTMLElement | undefined;
}) {
	let svg: SVGSVGElement | undefined;
	// Node y-centres, measured from the DOM rows (the rail can't know row heights
	// ahead of layout), plus the timeline height the SVG must cover.
	const [centers, setCenters] = createSignal<number[]>([]);
	const [height, setHeight] = createSignal(0);

	const railW = () => railWidth(props.graph);

	function measure() {
		const root = props.measureRoot();
		if (!root || !svg) return;
		const top = svg.getBoundingClientRect().top;
		const nodes = Array.from(root.querySelectorAll<HTMLElement>("[data-node]"));
		setCenters(
			nodes.map((n) => {
				const r = n.getBoundingClientRect();
				return (r.top + r.bottom) / 2 - top;
			}),
		);
		setHeight(root.getBoundingClientRect().height);
	}

	// Re-measure whenever the data changes; queueMicrotask lets Solid finish
	// patching the rows (and the browser lay them out) first. defer: onMount does
	// the first measure, so skip on()'s initial run to avoid measuring twice.
	createEffect(
		on(
			() => [props.entries, props.graph],
			() => queueMicrotask(measure),
			{ defer: true },
		),
	);

	onMount(() => {
		queueMicrotask(measure);
		const root = props.measureRoot();
		if (root) {
			const ro = new ResizeObserver(() => measure());
			ro.observe(root);
			onCleanup(() => ro.disconnect());
		}
		window.addEventListener("resize", measure);
		onCleanup(() => window.removeEventListener("resize", measure));
	});

	const primitives = createMemo<Prim[]>(() => {
		const ys = centers();
		const es = props.entries;
		const g = props.graph;
		if (es.length === 0 || ys.length !== es.length) return [];
		const y = (i: number) => ys[i] ?? 0;
		const lane = (i: number) => g.nodeLane[i] ?? 0;
		const out: Prim[] = [];

		// Revert brackets: a dashed vertical in the branch lane.
		for (const s of g.spans) {
			const x = laneX(s.lane - 1);
			out.push({
				type: "line",
				x1: x,
				y1: y(s.top) + NODE_R + 2,
				x2: x,
				y2: y(s.bottom) - NODE_R - 2,
				stroke: "var(--gx-faint)",
				w: 1.2,
				dash: "1.5 3.5",
				faded: true,
			});
		}

		// Spines between adjacent nodes, tinted by the lower node's run state. A
		// blank spine (last row, or dropping into a tombstone) is skipped.
		for (let i = 0; i < es.length - 1; i++) {
			const cur = es[i];
			const next = es[i + 1];
			if (!cur || !next || next.deleted) continue;
			const xa = laneX(lane(i));
			const xb = laneX(lane(i + 1));
			const ya = y(i);
			const yb = y(i + 1);
			const col = RUN_VAR[runState(next)];
			if (cur.kind === "recreate") {
				// Terminus: a buffer bar caps the old line, which continues down.
				const yTerm = ya + 16;
				out.push({
					type: "line",
					x1: xa - 5,
					y1: yTerm,
					x2: xa + 5,
					y2: yTerm,
					stroke: col,
					w: SPINE_W,
				});
				out.push({
					type: "line",
					x1: xa,
					y1: yTerm,
					x2: xb,
					y2: yb,
					stroke: col,
					w: SPINE_W,
				});
			} else if (xa === xb) {
				out.push({
					type: "line",
					x1: xa,
					y1: ya,
					x2: xb,
					y2: yb,
					stroke: col,
					w: SPINE_W,
				});
			} else if (xb > xa) {
				out.push({
					type: "path",
					d: stepPath(xa, ya, xb, yb, ya + STEP),
					stroke: col,
					w: SPINE_W,
				});
			} else {
				out.push({
					type: "path",
					d: stepPath(xa, ya, xb, yb, yb - STEP),
					stroke: col,
					w: SPINE_W,
				});
			}
		}

		// Nodes: an X for a tombstone, a hollow ring for disabled, else a dot.
		es.forEach((e, i) => {
			const x = laneX(lane(i));
			const cy = y(i);
			const col = RUN_VAR[runState(e)];
			if (e.deleted) {
				out.push({
					type: "line",
					x1: x - 4,
					y1: cy - 4,
					x2: x + 4,
					y2: cy + 4,
					stroke: col,
					w: 1.8,
				});
				out.push({
					type: "line",
					x1: x - 4,
					y1: cy + 4,
					x2: x + 4,
					y2: cy - 4,
					stroke: col,
					w: 1.8,
				});
			} else if (runState(e) === "disabled") {
				out.push({ type: "ring", cx: x, cy, stroke: col });
			} else {
				out.push({ type: "dot", cx: x, cy, fill: col });
			}
		});
		return out;
	});

	return (
		<svg
			ref={svg}
			class={styles.rail}
			width={railW()}
			height={height()}
			aria-hidden="true"
		>
			<For each={primitives()}>
				{(p) =>
					p.type === "path" ? (
						<path
							d={p.d}
							fill="none"
							stroke={p.stroke}
							stroke-width={p.w}
							stroke-linejoin="round"
						/>
					) : p.type === "dot" ? (
						<circle cx={p.cx} cy={p.cy} r={NODE_R} fill={p.fill} />
					) : p.type === "ring" ? (
						<circle
							cx={p.cx}
							cy={p.cy}
							r={NODE_R}
							fill="var(--vscode-editor-background, transparent)"
							stroke={p.stroke}
							stroke-width={1.6}
						/>
					) : (
						<line
							x1={p.x1}
							y1={p.y1}
							x2={p.x2}
							y2={p.y2}
							stroke={p.stroke}
							stroke-width={p.w}
							stroke-linecap="round"
							stroke-dasharray={p.dash}
							style={p.faded ? { "stroke-opacity": "0.55" } : undefined}
						/>
					)
				}
			</For>
		</svg>
	);
}
