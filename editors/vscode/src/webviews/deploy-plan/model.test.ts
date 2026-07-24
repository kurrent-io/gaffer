import { describe, expect, it } from "vitest";
import type { DeploySummaryCounts, PlanItem } from "./protocol";
import {
	doneHeadline,
	planLabel,
	planSummarySegments,
	resultSegments,
	rowTags,
} from "./model";

function item(
	p: Partial<PlanItem> & { name: string; outcome: string },
): PlanItem {
	return p;
}

function summary(p: Partial<DeploySummaryCounts>): DeploySummaryCounts {
	return {
		created: 0,
		updated: 0,
		rebuilt: 0,
		skipped: 0,
		refused: 0,
		invalid: 0,
		failed: 0,
		...p,
	};
}

describe("planLabel", () => {
	it("shows the would-do verb, refused reads as recreate", () => {
		expect(planLabel("created")).toBe("create");
		expect(planLabel("refused")).toBe("recreate");
		expect(planLabel("skipped")).toBe("unchanged");
		expect(planLabel("mystery")).toBe("mystery");
	});
});

describe("planSummarySegments", () => {
	it("counts per outcome in plan-action order, skipping zeros", () => {
		const segs = planSummarySegments([
			item({ name: "a", outcome: "created" }),
			item({ name: "b", outcome: "updated" }),
			item({ name: "c", outcome: "updated" }),
			item({ name: "d", outcome: "skipped" }),
		]);
		expect(segs).toEqual([
			{ text: "1 to create", outcome: "created" },
			{ text: "2 to update", outcome: "updated" },
			{ text: "1 unchanged", outcome: "skipped" },
		]);
	});
});

describe("resultSegments", () => {
	it("renders past-tense counts from the summary", () => {
		expect(resultSegments(summary({ created: 2, failed: 1 }))).toEqual([
			{ text: "2 created", outcome: "created" },
			{ text: "1 failed", outcome: "failed" },
		]);
	});
});

describe("doneHeadline", () => {
	it("leads with a failure, else notes a partial deploy, else success", () => {
		expect(doneHeadline(summary({ created: 1, failed: 2 }))).toEqual({
			text: "Deployed with 2 failed",
			ok: false,
		});
		expect(doneHeadline(summary({ created: 1, invalid: 1 }))).toEqual({
			text: "Deployed the valid projections",
			ok: true,
		});
		expect(doneHeadline(summary({ created: 3 }))).toEqual({
			text: "Successfully deployed",
			ok: true,
		});
	});
});

describe("rowTags", () => {
	it("lists the warning flags and an external-change attribution", () => {
		expect(
			rowTags(
				item({
					name: "a",
					outcome: "updated",
					faulted: true,
					logicChange: true,
					externalChange: true,
					externalChangeTool: "terraform",
				}),
			),
		).toEqual(["faulted", "logic change", "changed by terraform"]);
		expect(rowTags(item({ name: "b", outcome: "created" }))).toEqual([]);
	});
});
