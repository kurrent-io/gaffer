// Pure display logic for the deploy plan, ported from the old inline script:
// the would-do verb per outcome, the plan/result count roll-ups, the row
// warning tags, and the completion headline. Framework-free and unit tested.

import type { DeploySummaryCounts, PlanItem } from "./protocol";

// A plan shows what a deploy WOULD do, not the past-tense result words the
// shared JSON outcome carries.
const PLAN_LABEL: Record<string, string> = {
	created: "create",
	updated: "update",
	rebuilt: "rebuild",
	skipped: "unchanged",
	refused: "recreate",
	invalid: "invalid",
	failed: "error",
};

export function planLabel(outcome: string): string {
	return PLAN_LABEL[outcome] ?? outcome;
}

// A count segment: display text plus the outcome it's coloured by (the caller
// maps outcome -> colour class).
export interface Seg {
	text: string;
	outcome: string;
}

// Per-action roll-up in plan-action order, one segment per non-zero outcome.
const SUMMARY_SEGS: [string, string][] = [
	["created", "to create"],
	["updated", "to update"],
	["rebuilt", "to rebuild"],
	["refused", "to recreate"],
	["skipped", "unchanged"],
	["invalid", "invalid"],
	["failed", "failed"],
];

export function planSummarySegments(items: PlanItem[]): Seg[] {
	const counts: Record<string, number> = {};
	for (const item of items)
		counts[item.outcome] = (counts[item.outcome] ?? 0) + 1;
	const out: Seg[] = [];
	for (const [outcome, label] of SUMMARY_SEGS) {
		const n = counts[outcome];
		if (n) out.push({ text: `${n} ${label}`, outcome });
	}
	return out;
}

// Past-tense breakdown for the completion line, from the summary counts.
const RESULT_SEGS: [keyof DeploySummaryCounts, string][] = [
	["created", "created"],
	["updated", "updated"],
	["rebuilt", "rebuilt"],
	["skipped", "unchanged"],
	["refused", "refused"],
	["invalid", "invalid"],
	["failed", "failed"],
];

export function resultSegments(summary: DeploySummaryCounts): Seg[] {
	const out: Seg[] = [];
	for (const [key, label] of RESULT_SEGS) {
		if (summary[key])
			out.push({ text: `${summary[key]} ${label}`, outcome: key });
	}
	return out;
}

// The completion headline. A bypassed blocked plan deploys the valid ones (not
// "successfully" - the invalid/refused didn't apply); any failure leads.
export function doneHeadline(summary: DeploySummaryCounts): {
	text: string;
	ok: boolean;
} {
	const failed = summary.failed || 0;
	const skipped = (summary.invalid || 0) + (summary.refused || 0);
	if (failed > 0) return { text: `Deployed with ${failed} failed`, ok: false };
	if (skipped > 0) return { text: "Deployed the valid projections", ok: true };
	return { text: "Successfully deployed", ok: true };
}

// The warning tags on a row. recreate isn't here - it's carried by the
// "refused" outcome itself.
const TAGS: [keyof PlanItem, string][] = [
	["faulted", "faulted"],
	["emittingReset", "re-emits"],
	["logicChange", "logic change"],
];

export function rowTags(item: PlanItem): string[] {
	const tags: string[] = [];
	for (const [key, text] of TAGS) if (item[key]) tags.push(text);
	if (item.externalChange) {
		tags.push(
			item.externalChangeTool
				? `changed by ${item.externalChangeTool}`
				: "changed externally",
		);
	}
	return tags;
}
