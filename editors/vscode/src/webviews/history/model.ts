// Pure display logic for the history timeline, ported from the old inline
// script. Maps a ledger entry to its verb, tone, provenance line, and the
// derived questions the view asks (is it rollable, what came before). Kept
// framework-free so it can be unit tested against the CLI's grammar.

import type { Tone } from "../shared/tone";
import type { RunState } from "../shared/StatusDot";
import type { HistoryEntry, HistoryGraph } from "./protocol";

export function runState(e: HistoryEntry): RunState {
	return e.deleted ? "deleted" : e.enabled ? "enabled" : "disabled";
}

// The verb read as a word, matching the CLI's eventLabel: a foreign write folds
// in the tool it came through.
export function verbLabel(e: HistoryEntry): string {
	switch (e.kind) {
		case "updated-by":
			return e.tool ? `updated via ${e.tool}` : "updated";
		case "unreadable":
			return "unreadable metadata";
		default:
			return e.kind;
	}
}

// Verb colour, matching the CLI's historyKindStyle: an out-of-band change or
// unreadable metadata warns; gaffer's ops and neutral create/update share the
// deploy tone; the rest are coloured by kind.
export function verbTone(e: HistoryEntry): Tone {
	if (e.outOfBand) return "warn";
	switch (e.kind) {
		case "deleted":
			return "deleted";
		case "enabled":
			return "enabled";
		case "unreadable":
			return "warn";
		case "deploy":
		case "rollback":
		case "reset":
		case "recreate":
		case "created":
		case "updated":
		case "updated-by":
			return "deploy";
		case "rewritten":
			return "rewrite";
		default:
			return "quiet";
	}
}

export function shortHash(h: string): string {
	return h && h.length >= 7 ? h.slice(0, 7) : h || "";
}

export function shortRev(r: string): string {
	return r.length > 10 ? r.slice(0, 8) : r;
}

// "2026-07-22T09:14:00Z" -> "2026-07-22 09:14"; blank stays a dash.
export function fmtTime(t: string): string {
	if (!t) return "—";
	return t.length >= 16 ? t.slice(0, 16).replace("T", " ") : t;
}

export interface Note {
	text: string;
	warn: boolean;
}

// The dimmed second line on a row: an out-of-band caution, a reconfigure's knob
// changes, or the deployer / tool / source revision.
export function provenance(e: HistoryEntry): Note {
	if (e.kind === "unreadable")
		return { text: "deploy metadata could not be read", warn: true };
	if (e.kind === "reconfigured") {
		const parts = (e.configChanges ?? []).map(
			(c) => `${c.knob} ${c.from} → ${c.to}`,
		);
		return { text: parts.join(" · "), warn: false };
	}
	if (e.changeSummary)
		return e.outOfBand
			? { text: `${e.changeSummary} outside gaffer`, warn: true }
			: { text: e.changeSummary, warn: false };
	const parts: string[] = [];
	if (e.actor) parts.push(e.actor);
	let via = e.tool ?? "";
	if (e.toolVersion) via += (via ? " " : "") + e.toolVersion;
	if (via) parts.push(via);
	if (e.revision) parts.push(`src ${shortRev(e.revision)}`);
	if (e.outOfBand)
		return {
			text: ["changed outside gaffer", ...parts].join(" · "),
			warn: true,
		};
	return { text: parts.join(" · "), warn: false };
}

// A content version is a rollback target with a source to diff; state changes
// and the tombstone are neither.
export function rollable(e: HistoryEntry): boolean {
	return !e.stateChange && !!e.contentHash;
}

// The detail pane's one-line note: what changed / why it's flagged. Attribution
// (actor/tool/source) lives in the metadata block instead, so this stays the
// "what", not the "who".
export function detailNote(e: HistoryEntry): Note | null {
	if (e.kind === "unreadable")
		return { text: "deploy metadata could not be read", warn: true };
	if (e.kind === "reconfigured")
		return {
			text: (e.configChanges ?? [])
				.map((c) => `${c.knob} ${c.from} → ${c.to}`)
				.join(" · "),
			warn: false,
		};
	if (e.changeSummary)
		return e.outOfBand
			? { text: `${e.changeSummary} outside gaffer`, warn: true }
			: { text: e.changeSummary, warn: false };
	if (e.outOfBand) return { text: "changed outside gaffer", warn: true };
	return null;
}

// The nearest older entry carrying a content hash - the version this one
// changed from. State changes and the tombstone carry no content, so skip them.
export function previousContent(
	entries: HistoryEntry[],
	version: number,
): HistoryEntry | undefined {
	const i = entries.findIndex((e) => e.version === version);
	if (i < 0) return undefined;
	for (let j = i + 1; j < entries.length; j++) {
		const e = entries[j];
		if (e?.contentHash) return e;
	}
	return undefined;
}

export function hasPreviousContent(
	entries: HistoryEntry[],
	version: number,
): boolean {
	return previousContent(entries, version) !== undefined;
}

// A version "matches an earlier deploy" when it's the newer end (top) of an
// accepted revert bracket - its content reappeared from an older version.
export function matchesEarlier(
	entries: HistoryEntry[],
	graph: HistoryGraph,
	version: number,
): boolean {
	const i = entries.findIndex((e) => e.version === version);
	return i >= 0 && graph.spans.some((s) => s.top === i);
}
