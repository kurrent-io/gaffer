import { describe, expect, it } from "vitest";
import * as v from "valibot";
import { CliMessageWireSchema } from "./schemas.js";

// Trust-boundary schema: every variant must round-trip a known-good
// payload and reject obviously malformed payloads. Drift between the
// CLI's wire format and these schemas would silently swallow messages
// in production (process.ts logs and discards them on safeParse fail);
// a test catches the drift.

describe("CliMessageWireSchema", () => {
	describe("info", () => {
		it("accepts a minimal projection metadata", () => {
			const r = v.safeParse(CliMessageWireSchema, {
				type: "info",
				projection: { name: "checkout" },
			});
			expect(r.success).toBe(true);
		});
		it("accepts the full projection metadata shape", () => {
			const r = v.safeParse(CliMessageWireSchema, {
				type: "info",
				projection: {
					name: "checkout",
					source: "all",
					partitioning: "by stream",
					events: ["OrderPlaced", "OrderShipped"],
					engineVersion: 2,
				},
			});
			expect(r.success).toBe(true);
		});
		it("rejects a missing projection.name", () => {
			const r = v.safeParse(CliMessageWireSchema, {
				type: "info",
				projection: { source: "all" },
			});
			expect(r.success).toBe(false);
		});
	});

	describe("event", () => {
		it("accepts a fully-populated event", () => {
			const r = v.safeParse(CliMessageWireSchema, {
				type: "event",
				sequenceNumber: 1,
				streamId: "orders-1",
				eventType: "OrderPlaced",
				data: { qty: 2 },
				metadata: { traceId: "abc" },
			});
			expect(r.success).toBe(true);
		});
		it("rejects a non-numeric sequenceNumber", () => {
			const r = v.safeParse(CliMessageWireSchema, {
				type: "event",
				sequenceNumber: "1",
				streamId: "orders-1",
				eventType: "OrderPlaced",
			});
			expect(r.success).toBe(false);
		});
	});

	describe("auth_required", () => {
		it("accepts an env", () => {
			const r = v.safeParse(CliMessageWireSchema, {
				type: "auth_required",
				env: "prod",
			});
			expect(r.success).toBe(true);
		});
		it("rejects a missing env", () => {
			const r = v.safeParse(CliMessageWireSchema, { type: "auth_required" });
			expect(r.success).toBe(false);
		});
	});

	describe("result", () => {
		it("accepts a processed result with optional fields", () => {
			const r = v.safeParse(CliMessageWireSchema, {
				type: "result",
				status: "processed",
				partition: "p1",
				state: { count: 1 },
				logs: ["hi"],
				emitted: [{ streamId: "s1" }],
			});
			expect(r.success).toBe(true);
		});
		it("accepts a skipped result", () => {
			const r = v.safeParse(CliMessageWireSchema, {
				type: "result",
				status: "skipped",
				reason: "filtered",
			});
			expect(r.success).toBe(true);
		});
		it("rejects skipped without reason", () => {
			const r = v.safeParse(CliMessageWireSchema, {
				type: "result",
				status: "skipped",
			});
			expect(r.success).toBe(false);
		});
		it("rejects an unknown result status", () => {
			const r = v.safeParse(CliMessageWireSchema, {
				type: "result",
				status: "unknown",
			});
			expect(r.success).toBe(false);
		});
	});

	describe("error / summary / debug", () => {
		it("accepts an error message", () => {
			const r = v.safeParse(CliMessageWireSchema, {
				type: "error",
				code: "E_FOO",
				description: "boom",
			});
			expect(r.success).toBe(true);
		});
		it("accepts a summary", () => {
			const r = v.safeParse(CliMessageWireSchema, {
				type: "summary",
				handled: 3,
				skipped: 1,
				errors: 0,
			});
			expect(r.success).toBe(true);
		});
		it("accepts a debug port message", () => {
			const r = v.safeParse(CliMessageWireSchema, {
				type: "debug",
				port: 4711,
			});
			expect(r.success).toBe(true);
		});
	});

	describe("fatal_error", () => {
		it("accepts code+description without a file", () => {
			const r = v.safeParse(CliMessageWireSchema, {
				type: "fatal_error",
				code: "JS_ERROR",
				description: "bad",
			});
			expect(r.success).toBe(true);
		});
		it("accepts a fully-populated fatal_error", () => {
			const r = v.safeParse(CliMessageWireSchema, {
				type: "fatal_error",
				code: "JS_ERROR",
				description: "bad",
				file: "/p/projection.js",
				line: 12,
				column: 4,
				jsStack: "at handler",
				eventId: "evt-1",
			});
			expect(r.success).toBe(true);
		});
		it("rejects when description is missing", () => {
			const r = v.safeParse(CliMessageWireSchema, {
				type: "fatal_error",
				code: "JS_ERROR",
			});
			expect(r.success).toBe(false);
		});
	});

	describe("discriminator", () => {
		it("rejects an unknown type", () => {
			const r = v.safeParse(CliMessageWireSchema, {
				type: "nonsense",
			});
			expect(r.success).toBe(false);
		});
		it("rejects a missing type", () => {
			const r = v.safeParse(CliMessageWireSchema, {
				code: "noop",
			});
			expect(r.success).toBe(false);
		});
	});
});
