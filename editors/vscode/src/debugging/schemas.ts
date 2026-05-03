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

export const StateBodySchema = v.object({
	state: v.optional(v.unknown()),
	result: v.optional(v.unknown()),
	sharedState: v.optional(v.unknown()),
	partitions: v.optional(v.array(v.string())),
});
export type StateBody = v.InferOutput<typeof StateBodySchema>;

export const ModeBodySchema = v.object({
	mode: v.string(),
});
export type ModeBody = v.InferOutput<typeof ModeBodySchema>;

export const StatsBodySchema = v.object({
	handled: v.number(),
	errors: v.number(),
});
export type StatsBody = v.InferOutput<typeof StatsBodySchema>;
