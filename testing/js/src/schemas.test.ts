import { describe, expect, it } from "vitest";
import * as v from "valibot";
import type { RecordedEvent, ResolvedEvent } from "@kurrent/kurrentdb-client";
import {
	EventInputSchema,
	normalizeEvent,
	parseEventInput,
	RecordedEventSchema,
	ResolvedEventSchema,
	TestEventSchema,
} from "./schemas.js";

type Assert<T extends true> = T;
type Extends<A, B> = A extends B ? true : false;

// Compile-time: KurrentDB client types must be accepted by our schemas
it("schemas accept KurrentDB client types", () => {
	const recorded: Assert<
		Extends<RecordedEvent, v.InferInput<typeof RecordedEventSchema>>
	> = true;
	const resolved: Assert<
		Extends<Required<ResolvedEvent>, v.InferInput<typeof ResolvedEventSchema>>
	> = true;
	expect(recorded && resolved).toBe(true);
});

describe("TestEventSchema", () => {
	it("accepts minimal event", () => {
		const result = v.safeParse(TestEventSchema, {
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
		});
		expect(result.success).toBe(true);
	});

	it("accepts event with object data", () => {
		const result = v.safeParse(TestEventSchema, {
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			data: { foo: 1 },
		});
		expect(result.success).toBe(true);
	});

	it("accepts event with string data", () => {
		const result = v.safeParse(TestEventSchema, {
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			data: '{"foo":1}',
		});
		expect(result.success).toBe(true);
	});

	it("rejects circular data", () => {
		const obj: Record<string, unknown> = {};
		obj.self = obj;
		const result = v.safeParse(TestEventSchema, {
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			data: obj,
		});
		expect(result.success).toBe(false);
	});

	it("rejects BigInt data", () => {
		const result = v.safeParse(TestEventSchema, {
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			data: { n: 1n },
		});
		expect(result.success).toBe(false);
	});
});

describe("EventInputSchema", () => {
	it("accepts TestEvent shape", () => {
		const result = v.safeParse(EventInputSchema, {
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
		});
		expect(result.success).toBe(true);
	});

	it("accepts RecordedEvent shape", () => {
		const result = v.safeParse(EventInputSchema, {
			type: "Ping",
			streamId: "s-1",
			data: "{}",
		});
		expect(result.success).toBe(true);
	});

	it("accepts ResolvedEvent shape", () => {
		const result = v.safeParse(EventInputSchema, {
			event: { type: "Ping", streamId: "s-1", data: "{}" },
		});
		expect(result.success).toBe(true);
	});

	it("accepts full KurrentDB RecordedEvent", () => {
		const event: RecordedEvent = {
			streamId: "order-1",
			id: "550e8400-e29b-41d4-a716-446655440000",
			isJson: true,
			revision: 5n,
			type: "OrderPlaced",
			created: new Date("2026-01-15T10:30:00Z"),
			data: { amount: 99 },
			metadata: { $correlationId: "abc-123" },
		};
		const result = v.safeParse(EventInputSchema, event);
		expect(result.success).toBe(true);
	});

	it("accepts full KurrentDB ResolvedEvent", () => {
		const resolved: ResolvedEvent = {
			event: {
				streamId: "order-1",
				id: "550e8400-e29b-41d4-a716-446655440000",
				isJson: true,
				revision: 5n,
				type: "OrderPlaced",
				created: new Date("2026-01-15T10:30:00Z"),
				data: { amount: 99 },
				metadata: { $correlationId: "abc-123" },
			},
			commitPosition: 1024n,
		};
		const result = v.safeParse(EventInputSchema, resolved);
		expect(result.success).toBe(true);
	});

	it("accepts KurrentDB RecordedEvent with binary data", () => {
		const event: RecordedEvent = {
			streamId: "s-1",
			id: "550e8400-e29b-41d4-a716-446655440000",
			isJson: false,
			revision: 0n,
			type: "BinaryEvent",
			created: new Date(),
			data: new Uint8Array([123, 125]),
			metadata: undefined,
		};
		const result = v.safeParse(EventInputSchema, event);
		expect(result.success).toBe(true);
	});
});

describe("normalizeEvent", () => {
	it("normalizes TestEvent", () => {
		const result = normalizeEvent({
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			data: { count: 1 },
		});
		expect(result.eventType).toBe("Ping");
		expect(result.streamId).toBe("s-1");
		expect(result.data).toBe('{"count":1}');
	});

	it("normalizes RecordedEvent", () => {
		const result = normalizeEvent({
			type: "Ping",
			streamId: "s-1",
			revision: 0,
			isJson: true,
			id: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
			data: '{"count":1}',
		});
		expect(result.eventType).toBe("Ping");
		expect(result.data).toBe('{"count":1}');
	});

	it("normalizes ResolvedEvent", () => {
		const result = normalizeEvent({
			event: {
				type: "Ping",
				streamId: "s-1",
				revision: 0,
				isJson: true,
				id: "00000000-0000-0000-0000-000000000000",
				created: "2026-01-01T00:00:00Z",
				data: '{"count":1}',
			},
		});
		expect(result.eventType).toBe("Ping");
		expect(result.streamId).toBe("s-1");
	});

	it("handles Uint8Array data", () => {
		const result = normalizeEvent({
			type: "Ping",
			streamId: "s-1",
			revision: 0,
			isJson: true,
			id: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
			data: new TextEncoder().encode('{"count":1}'),
		});
		expect(result.data).toBe('{"count":1}');
	});

	it("passes string data through", () => {
		const result = normalizeEvent({
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			data: '{"raw":true}',
		});
		expect(result.data).toBe('{"raw":true}');
	});

	it("returns undefined for null data", () => {
		const result = normalizeEvent({
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			data: null,
		});
		expect(result.data).toBeUndefined();
	});

	it("stringifies metadata objects", () => {
		const result = normalizeEvent({
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			metadata: { correlationId: "abc" },
		});
		expect(result.metadata).toBe('{"correlationId":"abc"}');
	});

	it("maps revision to sequenceNumber", () => {
		const result = normalizeEvent({
			type: "Ping",
			streamId: "s-1",
			revision: 42,
			isJson: true,
			id: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
			data: "{}",
		});
		expect(result.sequenceNumber).toBe(42);
	});

	it("throws on RecordedEvent missing required fields", () => {
		expect(() =>
			normalizeEvent({ type: "Ping", streamId: "s-1", data: "{}" }),
		).toThrow();
	});

	it("normalizes full KurrentDB RecordedEvent", () => {
		const event: RecordedEvent = {
			streamId: "order-1",
			id: "550e8400-e29b-41d4-a716-446655440000",
			isJson: true,
			revision: 5n,
			type: "OrderPlaced",
			created: new Date(),
			data: { amount: 99 },
			metadata: { $correlationId: "abc" },
		};
		const parsed = v.parse(EventInputSchema, event);
		const result = normalizeEvent(parsed);
		expect(result.eventType).toBe("OrderPlaced");
		expect(result.streamId).toBe("order-1");
		expect(result.data).toBe('{"amount":99}');
		expect(result.metadata).toBe('{"$correlationId":"abc"}');
		expect(result.isJson).toBe(true);
		expect(result.sequenceNumber).toBe(5);
	});

	it("normalizes full KurrentDB ResolvedEvent", () => {
		const resolved: ResolvedEvent = {
			event: {
				streamId: "order-1",
				id: "550e8400-e29b-41d4-a716-446655440000",
				isJson: true,
				revision: 5n,
				type: "OrderPlaced",
				created: new Date(),
				data: { amount: 99 },
				metadata: { $correlationId: "abc" },
			},
			commitPosition: 1024n,
		};
		const parsed = v.parse(EventInputSchema, resolved);
		const result = normalizeEvent(parsed);
		expect(result.eventType).toBe("OrderPlaced");
		expect(result.streamId).toBe("order-1");
		expect(result.data).toBe('{"amount":99}');
	});

	it("handles event with both eventType and type fields", () => {
		const ambiguous = {
			eventType: "TestVersion",
			type: "ClientVersion",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			data: "{}",
		};
		const result = normalizeEvent(ambiguous);
		expect(result.eventType).toBe("TestVersion");
	});

	it("preserves eventId when provided on TestEvent", () => {
		const result = normalizeEvent({
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			eventId: "custom-id-123",
		});
		expect(result.eventId).toBe("custom-id-123");
	});

	it("generates eventId when not provided on TestEvent", () => {
		const result = normalizeEvent({
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
		});
		expect(result.eventId).toBeDefined();
		expect(result.eventId.length).toBeGreaterThan(0);
	});

	it("preserves timestamp when provided on TestEvent", () => {
		const result = normalizeEvent({
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			timestamp: "2026-01-15T10:00:00Z",
		});
		expect(result.timestamp).toBe("2026-01-15T10:00:00Z");
	});

	it("generates timestamp when not provided on TestEvent", () => {
		const result = normalizeEvent({
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
		});
		expect(result.timestamp).toBeDefined();
		expect(result.timestamp.length).toBeGreaterThan(0);
	});

	it("rejects invalid input", () => {
		expect(() => parseEventInput({ foo: "bar" } as never)).toThrow();
	});
});
