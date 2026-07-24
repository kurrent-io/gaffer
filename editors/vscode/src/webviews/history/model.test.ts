import { describe, expect, it } from "vitest";
import type { HistoryEntry, HistoryGraph } from "./protocol";
import {
	fmtTime,
	hasPreviousContent,
	matchesEarlier,
	previousContent,
	provenance,
	rollable,
	runState,
	shortHash,
	verbLabel,
	verbTone,
} from "./model";

function entry(p: Partial<HistoryEntry> & { version: number }): HistoryEntry {
	return {
		time: "",
		contentHash: "",
		kind: "updated",
		enabled: false,
		outOfBand: false,
		changeSummary: "",
		stateChange: false,
		deleted: false,
		...p,
	};
}

describe("runState", () => {
	it("prefers deleted, then enabled, else disabled", () => {
		expect(runState(entry({ version: 1, deleted: true, enabled: true }))).toBe(
			"deleted",
		);
		expect(runState(entry({ version: 1, enabled: true }))).toBe("enabled");
		expect(runState(entry({ version: 1 }))).toBe("disabled");
	});
});

describe("verbLabel", () => {
	it("folds the tool into a foreign write, else reads the kind", () => {
		expect(
			verbLabel(entry({ version: 1, kind: "updated-by", tool: "terraform" })),
		).toBe("updated via terraform");
		expect(verbLabel(entry({ version: 1, kind: "updated-by" }))).toBe(
			"updated",
		);
		expect(verbLabel(entry({ version: 1, kind: "unreadable" }))).toBe(
			"unreadable metadata",
		);
		expect(verbLabel(entry({ version: 1, kind: "deploy" }))).toBe("deploy");
	});
});

describe("verbTone", () => {
	it("warns on out-of-band regardless of kind", () => {
		expect(
			verbTone(entry({ version: 1, kind: "deploy", outOfBand: true })),
		).toBe("warn");
	});
	it("colours gaffer content ops as deploy and others by kind", () => {
		expect(verbTone(entry({ version: 1, kind: "rollback" }))).toBe("deploy");
		expect(verbTone(entry({ version: 1, kind: "deleted" }))).toBe("deleted");
		expect(verbTone(entry({ version: 1, kind: "enabled" }))).toBe("enabled");
		expect(verbTone(entry({ version: 1, kind: "rewritten" }))).toBe("rewrite");
		expect(verbTone(entry({ version: 1, kind: "reconfigured" }))).toBe("quiet");
	});
});

describe("fmtTime / shortHash", () => {
	it("formats an ISO stamp and dashes a blank", () => {
		expect(fmtTime("2026-07-22T09:14:00Z")).toBe("2026-07-22 09:14");
		expect(fmtTime("")).toBe("—");
	});
	it("truncates a hash to 7 chars", () => {
		expect(shortHash("0123456789")).toBe("0123456");
		expect(shortHash("")).toBe("");
	});
});

describe("provenance", () => {
	it("flags a change summary made outside gaffer", () => {
		const p = provenance(
			entry({ version: 1, changeSummary: "query changed", outOfBand: true }),
		);
		expect(p).toEqual({ text: "query changed outside gaffer", warn: true });
	});
	it("joins actor / tool / source for a normal write", () => {
		const p = provenance(
			entry({
				version: 1,
				actor: "george",
				tool: "gaffer",
				toolVersion: "1.2.0",
				revision: "abcdef123456",
			}),
		);
		expect(p).toEqual({
			text: "george · gaffer 1.2.0 · src abcdef12",
			warn: false,
		});
	});
	it("renders reconfigure knob changes", () => {
		const p = provenance(
			entry({
				version: 1,
				kind: "reconfigured",
				configChanges: [{ knob: "checkpoint", from: "10", to: "20" }],
			}),
		);
		expect(p).toEqual({ text: "checkpoint 10 → 20", warn: false });
	});
});

describe("rollable / previousContent", () => {
	const v3 = entry({ version: 3, contentHash: "hhh3333" });
	const v2 = entry({ version: 2, stateChange: true, kind: "disabled" });
	const v1 = entry({ version: 1, contentHash: "hhh1111" });
	const entries = [v3, v2, v1];
	it("is rollable only for a content version", () => {
		expect(rollable(v3)).toBe(true);
		expect(rollable(v2)).toBe(false);
	});
	it("skips state changes to find the previous content", () => {
		expect(previousContent(entries, 3)?.version).toBe(1);
		expect(hasPreviousContent(entries, 1)).toBe(false);
	});
});

describe("matchesEarlier", () => {
	it("is true for the top endpoint of a revert span", () => {
		const entries = [
			entry({ version: 3, contentHash: "aaaa111" }),
			entry({ version: 2, contentHash: "bbbb222" }),
			entry({ version: 1, contentHash: "aaaa111" }),
		];
		const graph: HistoryGraph = {
			nodeLane: [0, 1, 0],
			spans: [{ top: 0, bottom: 2, lane: 1 }],
		};
		expect(matchesEarlier(entries, graph, 3)).toBe(true);
		expect(matchesEarlier(entries, graph, 2)).toBe(false);
	});
});
