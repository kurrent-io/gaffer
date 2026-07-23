// The wire shapes the history viewer reads from the CLI's cold spawns: the
// deploy ledger from `gaffer history --json` and a `gaffer rollback --json`
// result. Validated at the spawn boundary like every other CLI payload - we
// don't trust the wire format - so the panel renders a known shape. (The
// per-entry diff goes over the LSP warm connection instead; see lsp/diff.ts.)

import * as v from "valibot";

// One config knob that moved between versions, in its native unit.
export const ConfigChangeSchema = v.object({
	knob: v.string(),
	from: v.string(),
	to: v.string(),
});

// One version in the ledger (cliout.HistoryJSON). `--json` is uncollapsed: every
// stream write is an entry, so recreate bookends aren't folded here (the viewer
// folds them, see history-graph.ts). `contentHash` is the full hash - the ref
// handle for a diff or rollback. Fields the CLI omits when false/empty are
// optional. `stateChange` marks a write that carries no new content identity
// (enable/disable/reconfigure/rewrite/reset/delete).
export const HistoryEntrySchema = v.object({
	version: v.number(),
	time: v.optional(v.string(), ""),
	contentHash: v.optional(v.string(), ""),
	kind: v.string(),
	enabled: v.optional(v.boolean(), false),
	outOfBand: v.optional(v.boolean(), false),
	stateChange: v.optional(v.boolean(), false),
	deleted: v.optional(v.boolean(), false),
	tool: v.optional(v.string()),
	toolVersion: v.optional(v.string()),
	operation: v.optional(v.string()),
	actor: v.optional(v.string()),
	revision: v.optional(v.string()),
	configChanges: v.optional(v.array(ConfigChangeSchema)),
});
export type HistoryEntry = v.InferOutput<typeof HistoryEntrySchema>;

export const HistoryReportSchema = v.array(HistoryEntrySchema);

// A rollback result (cli rollbackJSON). `outcome` is "rolled-back" when the live
// query moved, "unchanged" when the target was already current.
export const RollbackResultSchema = v.object({
	name: v.string(),
	outcome: v.picklist(["rolled-back", "unchanged"]),
	hash: v.string(),
});
export type RollbackResult = v.InferOutput<typeof RollbackResultSchema>;

function parse<TSchema extends v.GenericSchema>(
	schema: TSchema,
	stdout: string,
): v.InferOutput<TSchema> | null {
	let raw: unknown;
	try {
		raw = JSON.parse(stdout);
	} catch {
		return null;
	}
	const result = v.safeParse(schema, raw);
	return result.success ? result.output : null;
}

export const parseHistoryReport = (stdout: string): HistoryEntry[] | null =>
	parse(HistoryReportSchema, stdout);

export const parseRollbackResult = (stdout: string): RollbackResult | null =>
	parse(RollbackResultSchema, stdout);
