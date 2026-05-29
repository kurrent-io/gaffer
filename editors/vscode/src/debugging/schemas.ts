// Schemas for DAP custom events emitted by the gaffer CLI's DAP server.
//
// Validated per-event in dap-dispatch.ts. Shared shapes (InputEvent,
// EmittedEvent, StepResult) originate in ipc/schemas.ts since the same
// types appear in CLI NDJSON. Schemas for panel-initiated DAP requests
// live with their consumer in panels/schemas.ts.

import * as v from "valibot";
import {
	EmittedEventSchema,
	InputEventSchema,
	StepResultSchema,
} from "../ipc/schemas.js";

export const StepStartBodySchema = v.object({ event: InputEventSchema });
export type StepStartBody = v.InferOutput<typeof StepStartBodySchema>;

export const StepLogBodySchema = v.object({ message: v.string() });
export type StepLogBody = v.InferOutput<typeof StepLogBodySchema>;

export const StepEmitBodySchema = EmittedEventSchema;
export type StepEmitBody = v.InferOutput<typeof StepEmitBodySchema>;

export const StepResultBodySchema = v.object({ result: StepResultSchema });
export type StepResultBody = v.InferOutput<typeof StepResultBodySchema>;

export const StepErrorBodySchema = v.object({
	code: v.string(),
	description: v.string(),
});
export type StepErrorBody = v.InferOutput<typeof StepErrorBodySchema>;

// A runtime quirk that fired while processing the event. step and severity
// ride along from the CLI but the step panel keys off code + message; they
// are kept optional so a future consumer (e.g. severity-driven icon) can use
// them without a schema change.
export const StepWarningBodySchema = v.object({
	code: v.string(),
	message: v.string(),
	step: v.optional(v.number()),
	severity: v.optional(v.number()),
});
export type StepWarningBody = v.InferOutput<typeof StepWarningBodySchema>;

export const StateBodySchema = v.object({
	state: v.optional(v.unknown()),
	result: v.optional(v.unknown()),
	sharedState: v.optional(v.unknown()),
	partitions: v.optional(v.array(v.string())),
});
export type StateBody = v.InferOutput<typeof StateBodySchema>;

// Final-state snapshot sent just before the session terminates.
// Same shape as a partition's customRequest response (state / result),
// but keyed by partition name and accompanied by the unpartitioned /
// shared fields, mirroring engine.StateSummary.ToMap on the CLI side.
export const FinalStateBodySchema = v.object({
	state: v.optional(v.unknown()),
	result: v.optional(v.unknown()),
	sharedState: v.optional(v.unknown()),
	partitions: v.optional(
		v.record(
			v.string(),
			v.object({
				state: v.optional(v.unknown()),
				result: v.optional(v.unknown()),
			}),
		),
	),
});
export type FinalStateBody = v.InferOutput<typeof FinalStateBodySchema>;

export const ModeBodySchema = v.object({
	mode: v.string(),
});
export type ModeBody = v.InferOutput<typeof ModeBodySchema>;

// skipped + skippedByReason are emitted only in fixture mode; live mode
// drops are engine pre-filter noise.
const NonNegativeInt = v.pipe(v.number(), v.integer(), v.minValue(0));
export const StatsBodySchema = v.object({
	handled: v.number(),
	errors: v.number(),
	skipped: v.optional(NonNegativeInt),
	skippedByReason: v.optional(v.record(v.string(), NonNegativeInt)),
	// Distinct runtime-quirk codes seen so far; omitted when none have fired.
	quirks: v.optional(NonNegativeInt),
});
export type StatsBody = v.InferOutput<typeof StatsBodySchema>;

export const CaughtUpBodySchema = v.object({
	caughtUp: v.boolean(),
});
export type CaughtUpBody = v.InferOutput<typeof CaughtUpBodySchema>;
