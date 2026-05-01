// Schemas for the gaffer CLI's NDJSON output (one object per stdout line).
//
// Schemas live alongside their type aliases (`v.InferOutput<typeof S>`) so
// the runtime validator and the static type can't drift. Validated at the
// JSON.parse boundary in process.ts - we do not trust the wire format.

import * as v from "valibot";

// -- Shared shapes (also used by debugging/schemas.ts) --------------------

export const ProjectionMetadataSchema = v.object({
	name: v.string(),
	source: v.optional(v.string()),
	partitioning: v.optional(v.string()),
	events: v.optional(v.array(v.string())),
	engineVersion: v.optional(v.number()),
});
export type ProjectionMetadata = v.InferOutput<typeof ProjectionMetadataSchema>;

export const InputEventSchema = v.object({
	sequenceNumber: v.number(),
	streamId: v.string(),
	eventType: v.string(),
	data: v.optional(v.unknown()),
	metadata: v.optional(v.unknown()),
});
export type InputEvent = v.InferOutput<typeof InputEventSchema>;

export const EmittedEventSchema = v.object({
	streamId: v.string(),
	eventType: v.optional(v.string()),
	data: v.optional(v.unknown()),
	metadata: v.optional(v.unknown()),
	isLink: v.optional(v.boolean()),
});
export type EmittedEvent = v.InferOutput<typeof EmittedEventSchema>;

export const ProcessedResultSchema = v.object({
	status: v.literal("processed"),
	partition: v.optional(v.string()),
	state: v.optional(v.unknown()),
	result: v.optional(v.unknown()),
	logs: v.optional(v.array(v.string())),
	emitted: v.optional(v.array(EmittedEventSchema)),
});
export type ProcessedResult = v.InferOutput<typeof ProcessedResultSchema>;

export const SkippedResultSchema = v.object({
	status: v.literal("skipped"),
	reason: v.string(),
});
export type SkippedResult = v.InferOutput<typeof SkippedResultSchema>;

export const StepResultSchema = v.union([
	ProcessedResultSchema,
	SkippedResultSchema,
]);
export type StepResult = v.InferOutput<typeof StepResultSchema>;

export const SummaryStatsSchema = v.object({
	handled: v.number(),
	skipped: v.number(),
	errors: v.number(),
});
export type SummaryStats = v.InferOutput<typeof SummaryStatsSchema>;

// -- CLI NDJSON message variants ------------------------------------------

const InfoMessageSchema = v.object({
	type: v.literal("info"),
	projection: ProjectionMetadataSchema,
});

const EventMessageSchema = v.object({
	type: v.literal("event"),
	sequenceNumber: v.number(),
	streamId: v.string(),
	eventType: v.string(),
	data: v.optional(v.unknown()),
	metadata: v.optional(v.unknown()),
});

const ProcessedResultMessageSchema = v.object({
	type: v.literal("result"),
	status: v.literal("processed"),
	partition: v.optional(v.string()),
	state: v.optional(v.unknown()),
	result: v.optional(v.unknown()),
	logs: v.optional(v.array(v.string())),
	emitted: v.optional(v.array(EmittedEventSchema)),
});

const SkippedResultMessageSchema = v.object({
	type: v.literal("result"),
	status: v.literal("skipped"),
	reason: v.string(),
});

const ErrorMessageSchema = v.object({
	type: v.literal("error"),
	code: v.string(),
	description: v.string(),
});

const SummaryMessageSchema = v.object({
	type: v.literal("summary"),
	handled: v.number(),
	skipped: v.number(),
	errors: v.number(),
});

const DebugMessageSchema = v.object({
	type: v.literal("debug"),
	port: v.number(),
});

const FatalErrorMessageSchema = v.object({
	type: v.literal("fatal_error"),
	code: v.string(),
	description: v.string(),
	file: v.optional(v.string()),
	line: v.optional(v.number()),
	column: v.optional(v.number()),
	jsStack: v.optional(v.string()),
	eventId: v.optional(v.string()),
});

// CLI-emitted messages as they appear on stdout. v.variant is O(1) on the
// discriminator vs v.union's linear try-each.
export const CliMessageWireSchema = v.variant("type", [
	InfoMessageSchema,
	EventMessageSchema,
	ProcessedResultMessageSchema,
	SkippedResultMessageSchema,
	ErrorMessageSchema,
	SummaryMessageSchema,
	DebugMessageSchema,
	FatalErrorMessageSchema,
]);
export type CliMessageWire = v.InferOutput<typeof CliMessageWireSchema>;

// Synthesized by GafferSession on child-process exit; never on the wire.
export interface ExitMessage {
	type: "exit";
	code: number | null;
}

// CliMessage as seen by host listeners - includes the synthetic exit variant.
export type CliMessage = CliMessageWire | ExitMessage;
export type CliMessageType = CliMessage["type"];
