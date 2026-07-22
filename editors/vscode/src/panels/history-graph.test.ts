import { describe, expect, it } from "vitest";
import type { HistoryEntry } from "../commands/history-schema.js";
import {
	collapseHistory,
	computeHistoryGraph,
	HISTORY_MAX_LANES,
	type GraphNode,
} from "./history-graph.js";

// graphOf lays out a timeline from a newest-first list of content hashes. An
// empty string is a state change (no content identity) - matching cli/cmd's
// graphOf test helper.
function graphOf(...hashes: string[]) {
	return computeHistoryGraph(hashes.map((h) => ({ contentHash: h })));
}

describe("computeHistoryGraph", () => {
	it("no reverts: every node on the main line", () => {
		const g = graphOf("aaa", "bbb", "ccc");
		expect(g.spans).toHaveLength(0);
		expect(g.nodeLane).toEqual([0, 0, 0]);
	});

	it("simple revert: endpoints on main, detour in lane 1", () => {
		// aaa reappears at row 0, matching row 3; rows 1-2 are the detour.
		const g = graphOf("aaa", "bbb", "", "aaa");
		expect(g.spans).toEqual([{ top: 0, bottom: 3, lane: 1 }]);
		expect(g.nodeLane).toEqual([0, 1, 1, 0]);
	});

	it("adjacent identical write is a rewrite, not a branch", () => {
		const g = graphOf("aaa", "aaa", "bbb");
		expect(g.spans).toHaveLength(0);
	});

	it("sequential reverts sharing an endpoint share a lane", () => {
		// aaa at rows 0, 2, 5: two back-to-back reverts, both top-level in lane 1.
		const g = graphOf("aaa", "x", "aaa", "y", "z", "aaa");
		expect(g.spans).toHaveLength(2);
		for (const s of g.spans) expect(s.lane).toBe(1);
		expect(g.nodeLane).toEqual([0, 1, 0, 1, 1, 0]);
	});

	it("nested revert: inner detour one lane deeper", () => {
		// Outer aaa (0..6) with an inner bbb revert (2..4) inside its detour.
		const g = graphOf("aaa", "d", "bbb", "e", "bbb", "f", "aaa");
		expect(g.spans).toContainEqual({ top: 0, bottom: 6, lane: 1 });
		expect(g.spans).toContainEqual({ top: 2, bottom: 4, lane: 2 });
		expect(g.nodeLane).toEqual([0, 1, 1, 2, 1, 1, 0]);
	});

	it("nesting cap drops the third level to flat", () => {
		// aaa 0/10 (lane1), bbb 2/8 (lane2), ccc 4/6 would be lane3 -> dropped.
		const g = graphOf(
			"aaa",
			"p",
			"bbb",
			"q",
			"ccc",
			"r",
			"ccc",
			"s",
			"bbb",
			"t",
			"aaa",
		);
		expect(g.spans.some((s) => s.top === 4 && s.bottom === 6)).toBe(false);
		for (const s of g.spans) expect(s.lane).toBeLessThan(HISTORY_MAX_LANES);
		expect(g.nodeLane[5]).toBeLessThan(HISTORY_MAX_LANES);
	});

	it("interleaving spans: the newer-top span wins the crossing", () => {
		// A at rows 0,2; B at rows 1,3 - the spans cross. A (top 0) keeps its bracket.
		const g = graphOf("A", "B", "A", "B");
		expect(g.spans).toEqual([{ top: 0, bottom: 2, lane: 1 }]);
		expect(g.nodeLane).toEqual([0, 1, 0, 0]);
	});

	it("empty history is an empty graph", () => {
		const g = graphOf();
		expect(g.spans).toHaveLength(0);
		expect(g.nodeLane).toHaveLength(0);
	});

	it("a state change can't anchor a revert", () => {
		// deploy b, disable (still b), deploy b: the content never diverged.
		const noDetour: GraphNode[] = [
			{ contentHash: "b" },
			{ contentHash: "b", stateChange: true },
			{ contentHash: "b" },
		];
		expect(computeHistoryGraph(noDetour).spans).toHaveLength(0);

		// deploy X, deploy Y, disable X, deploy X: a genuine revert on the X deploys.
		const realDetour: GraphNode[] = [
			{ contentHash: "X" },
			{ contentHash: "Y" },
			{ contentHash: "X", stateChange: true },
			{ contentHash: "X" },
		];
		expect(computeHistoryGraph(realDetour).spans).toEqual([
			{ top: 0, bottom: 3, lane: 1 },
		]);
	});
});

function entry(version: number, kind: string): HistoryEntry {
	return {
		version,
		time: "",
		contentHash: "",
		kind,
		enabled: false,
		external: false,
		stateChange: false,
		deleted: false,
	};
}

describe("collapseHistory", () => {
	it("folds a recreate's delete + disable bookends into the recreate row", () => {
		const rows = collapseHistory([
			entry(5, "recreate"),
			entry(4, "deleted"),
			entry(3, "disabled"),
			entry(2, "deploy"),
		]);
		expect(rows.map((r) => r.version)).toEqual([5, 2]);
		expect(rows[0]?.absorbed.map((a) => a.version)).toEqual([4, 3]);
		expect(rows[1]?.absorbed).toHaveLength(0);
	});

	it("folds delete + rewritten (already-disabled recreate)", () => {
		const rows = collapseHistory([
			entry(3, "recreate"),
			entry(2, "deleted"),
			entry(1, "rewritten"),
		]);
		expect(rows).toHaveLength(1);
		expect(rows[0]?.absorbed.map((a) => a.version)).toEqual([2, 1]);
	});

	it("folds only the delete when the next row isn't a disable/rewrite", () => {
		const rows = collapseHistory([
			entry(3, "recreate"),
			entry(2, "deleted"),
			entry(1, "deploy"),
		]);
		expect(rows.map((r) => r.version)).toEqual([3, 1]);
		expect(rows[0]?.absorbed.map((a) => a.version)).toEqual([2]);
	});

	it("does not fold a recreate without an adjacent delete", () => {
		const rows = collapseHistory([entry(2, "recreate"), entry(1, "disabled")]);
		expect(rows.map((r) => r.version)).toEqual([2, 1]);
		expect(rows[0]?.absorbed).toHaveLength(0);
	});
});
