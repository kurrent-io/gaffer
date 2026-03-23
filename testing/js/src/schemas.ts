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
	eventType: v.string(),
	streamId: v.string(),
	sequenceNumber: v.number(),
	isJson: v.boolean(),
	data: v.optional(jsonData),
	metadata: v.optional(jsonData),
	eventId: v.optional(v.string()),
	timestamp: v.optional(v.string()),
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

export const ResolvedEventSchema = v.object({
	event: RecordedEventSchema,
});

export const EventInputSchema = v.union([
	TestEventSchema,
	ResolvedEventSchema,
	RecordedEventSchema,
]);

export type TestEvent = v.InferOutput<typeof TestEventSchema>;
export type EventInput = TestEvent | RecordedEvent | ResolvedEvent;

type ParsedEventInput = v.InferOutput<typeof EventInputSchema>;

export function parseEventInput(input: EventInput): ParsedEventInput {
	return v.parse(EventInputSchema, input);
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
	timestamp: string;
	/** Event data as a JSON string. */
	data?: string;
	/** Event metadata as a JSON string. */
	metadata?: string;
}

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
	return {
		eventType: event.eventType,
		streamId: event.streamId,
		sequenceNumber: event.sequenceNumber,
		isJson: event.isJson,
		eventId: event.eventId ?? crypto.randomUUID(),
		timestamp: event.timestamp ?? new Date().toISOString(),
		data: stringifyData(event.data),
		metadata: stringifyData(event.metadata),
	};
}

function normalizeRecordedEvent(event: ParsedRecordedEvent): NormalizedEvent {
	return {
		eventType: event.type,
		streamId: event.streamId,
		sequenceNumber: event.revision as number,
		isJson: event.isJson as boolean,
		eventId: event.id as string,
		timestamp:
			event.created instanceof Date
				? event.created.toISOString()
				: (event.created as string),
		data: stringifyData(event.data),
		metadata: stringifyData(event.metadata),
	};
}

function stringifyData(value: unknown): string | undefined {
	if (value === undefined || value === null) return undefined;
	if (value instanceof Uint8Array) return new TextDecoder().decode(value);
	if (typeof value === "string") return value;
	return JSON.stringify(value);
}
