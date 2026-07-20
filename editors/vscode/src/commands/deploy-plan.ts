// The deploy plan a `gaffer deploy --dry-run --json` run returns (the CLI's
// cliout.PlanReportJSON envelope). Validated at the spawn boundary like every
// other CLI payload - we don't trust the wire format - so the panel renders a
// known shape.

import * as v from "valibot";

// One projection in the plan. `outcome` is the would-be verdict:
// created / updated / rebuilt / skipped / refused / invalid / failed. The flags
// are the warnings the webview surfaces; all are omitted by the CLI when false.
export const PlanItemSchema = v.object({
	name: v.string(),
	outcome: v.string(),
	recreate: v.optional(v.boolean()),
	logicChange: v.optional(v.boolean()),
	externalChange: v.optional(v.boolean()),
	externalChangeTool: v.optional(v.string()),
	faulted: v.optional(v.boolean()),
	emittingReset: v.optional(v.boolean()),
	reason: v.optional(v.string()),
	error: v.optional(v.string()),
});
export type PlanItem = v.InferOutput<typeof PlanItemSchema>;

// One diverging [database_config] knob: the server's live value vs the declared
// local one, in the knob's native unit.
export const ConfigDriftSchema = v.object({
	knob: v.string(),
	server: v.number(),
	local: v.number(),
});
export type ConfigDrift = v.InferOutput<typeof ConfigDriftSchema>;

// The envelope. `verdict` is what a real deploy would do: in-sync / deployable /
// blocked. `production` is absent when a pure no-op skipped the server round-trip
// that determines it. `configDrift` and `configDriftError` are mutually exclusive.
export const PlanReportSchema = v.object({
	env: v.optional(v.string()),
	target: v.optional(v.string()),
	production: v.optional(v.boolean()),
	verdict: v.string(),
	changes: v.number(),
	plan: v.array(PlanItemSchema),
	configDrift: v.optional(v.array(ConfigDriftSchema)),
	configDriftError: v.optional(v.string()),
});
export type PlanReport = v.InferOutput<typeof PlanReportSchema>;

// Parse a `deploy --dry-run --json` stdout payload, or null when it isn't the
// expected envelope (empty stdout on a spawn that never produced one, or a shape
// mismatch).
export function parsePlanReport(stdout: string): PlanReport | null {
	let raw: unknown;
	try {
		raw = JSON.parse(stdout);
	} catch {
		return null;
	}
	const result = v.safeParse(PlanReportSchema, raw);
	return result.success ? result.output : null;
}
