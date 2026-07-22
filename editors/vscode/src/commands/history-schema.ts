// The wire shapes the history viewer reads from the CLI: the deploy ledger from
// `gaffer history --json`, a source diff from `gaffer diff --json`, and a
// `gaffer rollback --json` result. Validated at the spawn boundary like every
// other CLI payload - we don't trust the wire format - so the panel and the
// graph model render a known shape.

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
	external: v.optional(v.boolean(), false),
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

// One diff line (deploy.DiffLine). `kind` maps to a row style; `[emphFrom,emphTo)`
// are half-open rune-aligned byte offsets into `text` bounding the intraline
// change (empty span => no intraline highlight). `oldN`/`newN` are 1-based, 0
// where the side has no line.
export const DiffLineSchema = v.object({
	kind: v.picklist(["equal", "removed", "added"]),
	oldN: v.number(),
	newN: v.number(),
	text: v.optional(v.string(), ""),
	emphFrom: v.optional(v.number(), 0),
	emphTo: v.optional(v.number(), 0),
});
export type DiffLine = v.InferOutput<typeof DiffLineSchema>;

const DiffSideSchema = v.object({
	ref: v.string(),
	hash: v.optional(v.string()),
	source: v.optional(v.string(), ""),
});

// A source diff (cliout.DiffJSON). The viewer renders `lines`; `left`/`right`
// carry the canonical source for the "Open in diff editor" pop. verdict/changes
// are absent on a version-to-version diff and ignored here.
export const DiffReportSchema = v.object({
	name: v.string(),
	left: DiffSideSchema,
	right: DiffSideSchema,
	lines: v.array(DiffLineSchema),
});
export type DiffReport = v.InferOutput<typeof DiffReportSchema>;

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

export const parseDiffReport = (stdout: string): DiffReport | null =>
	parse(DiffReportSchema, stdout);

export const parseRollbackResult = (stdout: string): RollbackResult | null =>
	parse(RollbackResultSchema, stdout);
