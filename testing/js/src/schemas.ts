import type { RecordedEvent, ResolvedEvent } from "@kurrent/kurrentdb-client";
import * as v from "valibot";

const jsonSafe = v.check((value: unknown) => {
	try {
		JSON.stringify(value);
		return true;
	} catch {
		return false;
	}
}, "Value is not JSON-serializable (circular references or BigInts are not supported)");

const jsonData = v.pipe(v.any(), jsonSafe);

export const TestEventSchema = v.object({
	eventType: v.pipe(v.string(), v.nonEmpty("eventType must not be empty")),
	streamId: v.pipe(v.string(), v.nonEmpty("streamId must not be empty")),
	sequenceNumber: v.pipe(
		v.number(),
		v.safeInteger("sequenceNumber must be a safe integer"),
		v.minValue(0, "sequenceNumber must be >= 0"),
	),
	isJson: v.boolean(),
	data: v.optional(jsonData),
	metadata: v.optional(jsonData),
	eventId: v.optional(v.string()),
	created: v.optional(v.string()),
});

export const RecordedEventSchema = v.object({
	type: v.string(),
	streamId: v.string(),
	data: v.union([v.string(), v.instance(Uint8Array), v.any()]),
	metadata: v.optional(v.any()),
	revision: v.optional(
		v.union([v.number(), v.pipe(v.bigint(), v.transform(Number))]),
	),
	id: v.optional(v.string()),
	isJson: v.optional(v.boolean()),
	created: v.optional(v.any()),
});

const StrictRecordedEventSchema = v.object({
	type: v.string(),
	streamId: v.string(),
	data: v.optional(v.union([v.string(), v.instance(Uint8Array), v.any()])),
	metadata: v.optional(v.any()),
	revision: v.union([v.number(), v.pipe(v.bigint(), v.transform(Number))]),
	id: v.string(),
	isJson: v.boolean(),
	created: v.union([v.string(), v.date()]),
});

export const ResolvedEventSchema = v.object({
	event: RecordedEventSchema,
});

export const EventInputSchema = v.union([
	TestEventSchema,
	ResolvedEventSchema,
	RecordedEventSchema,
]);

/** A manually constructed test event. Required fields: eventType, streamId, sequenceNumber, isJson. */
export type TestEvent = v.InferOutput<typeof TestEventSchema>;

/** An event to feed to a projection. Accepts a TestEvent, KurrentDB RecordedEvent, or ResolvedEvent. */
export type EventInput = TestEvent | RecordedEvent | ResolvedEvent;

type ParsedEventInput = v.InferOutput<typeof EventInputSchema>;

/**
 * Parse and validate an event input against the accepted schemas.
 *
 * Parsing against the {@link EventInputSchema} union directly reports the
 * unhelpful "Expected Object but received Object" when nothing matches,
 * because all three branches are objects. Discriminate on each shape's
 * distinguishing key first, then parse against that single schema so the
 * error names the field that's actually wrong.
 *
 * @throws {ValiError} If the input matches a shape but a field is invalid.
 * @throws {Error} If the input matches no known event shape.
 */
export function parseEventInput(input: EventInput): ParsedEventInput {
	if (typeof input === "object" && input !== null) {
		if ("eventType" in input) return v.parse(TestEventSchema, input);
		if ("event" in input) return v.parse(ResolvedEventSchema, input);
		if ("type" in input) return v.parse(RecordedEventSchema, input);
	}
	throw new Error(
		`Unrecognized event shape: ${JSON.stringify(input)}. Expected a TestEvent (eventType), a KurrentDB RecordedEvent (type), or a ResolvedEvent (event).`,
	);
}

/** Event fields normalized to strings for the runtime C API. */
export interface NormalizedEvent {
	/** Event type name (e.g. "OrderPlaced"). */
	eventType: string;
	/** Stream the event belongs to. */
	streamId: string;
	/** Position of this event in its stream. */
	sequenceNumber: number;
	/** Whether the event data is JSON-encoded. */
	isJson: boolean;
	/** Unique event identifier (UUID). */
	eventId: string;
	/** When the event was created. */
	created: string;
	/** Event data as a JSON string. */
	data?: string;
	/** Event metadata as a JSON string. */
	metadata?: string;
}

/**
 * Normalize a parsed event input to the flat format expected by the runtime.
 * Handles TestEvent, RecordedEvent, and ResolvedEvent shapes.
 * @throws {Error} If the input doesn't match any recognized event shape.
 * @throws {ValiError} If a RecordedEvent is missing required fields (revision, isJson, id, created).
 */
export function normalizeEvent(input: ParsedEventInput): NormalizedEvent {
	if (v.is(TestEventSchema, input)) {
		return normalizeTestEvent(input);
	}

	if (v.is(ResolvedEventSchema, input)) {
		return normalizeRecordedEvent(input.event);
	}

	if (v.is(RecordedEventSchema, input)) {
		return normalizeRecordedEvent(input);
	}

	throw new Error(
		`Unrecognized event shape: ${JSON.stringify(input)}. Expected a TestEvent, RecordedEvent, or ResolvedEvent.`,
	);
}

type ParsedTestEvent = v.InferOutput<typeof TestEventSchema>;
type ParsedRecordedEvent = v.InferOutput<typeof RecordedEventSchema>;

function normalizeTestEvent(event: ParsedTestEvent): NormalizedEvent {
	const out: NormalizedEvent = {
		eventType: event.eventType,
		streamId: event.streamId,
		sequenceNumber: event.sequenceNumber,
		isJson: event.isJson,
		eventId: event.eventId ?? crypto.randomUUID(),
		created: event.created ?? new Date().toISOString(),
	};
	const data = stringifyData(event.data);
	if (data !== undefined) out.data = data;
	const metadata = stringifyData(event.metadata);
	if (metadata !== undefined) out.metadata = metadata;
	return out;
}

function normalizeRecordedEvent(event: ParsedRecordedEvent): NormalizedEvent {
	const strict = v.parse(StrictRecordedEventSchema, event);
	const out: NormalizedEvent = {
		eventType: strict.type,
		streamId: strict.streamId,
		sequenceNumber: strict.revision,
		isJson: strict.isJson,
		eventId: strict.id,
		created:
			strict.created instanceof Date
				? strict.created.toISOString()
				: strict.created,
	};
	const data = stringifyData(strict.data);
	if (data !== undefined) out.data = data;
	const metadata = stringifyData(strict.metadata);
	if (metadata !== undefined) out.metadata = metadata;
	return out;
}

function stringifyData(value: unknown): string | undefined {
	if (value === undefined || value === null) return undefined;
	if (value instanceof Uint8Array) return new TextDecoder().decode(value);
	if (typeof value === "string") return value;
	return JSON.stringify(value);
}
