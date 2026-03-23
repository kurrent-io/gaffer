import { describe, expect, it } from "vitest";
import { createProjection } from "./createProjection.js";
import {
	InvalidProjectionError,
	CompilationTimeoutError,
} from "@kurrent/gaffer-runtime";
import { systemEvents } from "./systemEvents.js";

const counterSource = `
	fromAll().when({
		$init: function() { return { count: 0 }; },
		ItemAdded: function(s, e) { s.count++; return s; }
	})
`;

describe("createProjection", () => {
	describe("validate", () => {
		it("returns valid for good projection", () => {
			const projection = createProjection(counterSource);
			const result = projection.validate();
			expect(result.valid).toBe(true);
			if (result.valid) {
				expect(result.info.source.type).toBe("all");
				expect(result.info.partitioning.type).toBe("none");
				expect(result.info.events).toContain("ItemAdded");
			}
		});

		it("returns invalid for bad JS", () => {
			const projection = createProjection("this is not valid {{{{");
			const result = projection.validate();
			expect(result.valid).toBe(false);
			if (!result.valid) {
				expect(result.error).toBeTruthy();
			}
		});

		it("maps foreachStream partitioning", () => {
			const projection = createProjection(`
				fromAll().foreachStream().when({
					$init: function() { return {}; },
					Ping: function(s, e) { return s; }
				})
			`);
			const result = projection.validate();
			expect(result.valid).toBe(true);
			if (result.valid) {
				expect(result.info.partitioning.type).toBe("byStream");
			}
		});

		it("maps category source", () => {
			const projection = createProjection(`
				fromCategory("orders").when({
					$init: function() { return {}; },
					Ping: function(s, e) { return s; }
				})
			`);
			const result = projection.validate();
			expect(result.valid).toBe(true);
			if (result.valid) {
				expect(result.info.source).toEqual({
					type: "categories",
					categories: ["orders"],
				});
			}
		});

		it("maps biState", () => {
			const projection = createProjection(`
				options({ biState: true });
				fromAll().when({
					$init: function() { return {}; },
					$initShared: function() { return {}; },
					Ping: function(s, e) { return s; }
				})
			`);
			const result = projection.validate();
			expect(result.valid).toBe(true);
			if (result.valid) {
				expect(result.info.biState).toBe(true);
			}
		});
	});

	describe("run (invalid)", () => {
		it("throws on invalid projection", () => {
			const projection = createProjection("this is not valid {{{{");
			expect(() => [...projection.run([])]).toThrow(InvalidProjectionError);
		});

		it("throws on invalid input type", () => {
			const projection = createProjection(counterSource);
			expect(() => [
				...projection.run(42 as unknown as Iterable<never>),
			]).toThrow("run() expects");
		});
	});

	describe("options", () => {
		it("passes compilation timeout to session", () => {
			const projection = createProjection("while(true) {}", {
				compilationTimeoutMs: 100,
			});
			expect(() => [...projection.run([])]).toThrow(CompilationTimeoutError);
		});

		it("v1 drops non-JSON events", () => {
			const projection = createProjection<{ count: number }>(counterSource, {
				version: "v1",
			});
			const results = [
				...projection.run([
					{
						eventType: "ItemAdded",
						streamId: "s-1",
						sequenceNumber: 0,
						isJson: false,
						data: "not json",
					},
					{
						eventType: "ItemAdded",
						streamId: "s-1",
						sequenceNumber: 1,
						isJson: true,
						data: { item: "a" },
					},
				]),
			];
			expect(results).toHaveLength(2);
			// First event (non-JSON) dropped by V1 - no state change
			expect(results[0].state).toBeNull();
			// Second event (JSON) processed normally
			expect(results[1].state?.count).toBe(1);
		});
	});

	describe("run (sync)", () => {
		it("iterates over events and yields state", () => {
			const projection = createProjection<{ count: number }>(counterSource);
			const events = [
				{
					eventType: "ItemAdded",
					streamId: "cart-1",
					sequenceNumber: 0,
					isJson: true,
					data: {},
				},
				{
					eventType: "ItemAdded",
					streamId: "cart-1",
					sequenceNumber: 1,
					isJson: true,
					data: {},
				},
				{
					eventType: "ItemAdded",
					streamId: "cart-1",
					sequenceNumber: 2,
					isJson: true,
					data: {},
				},
			];

			const steps = [...projection.run(events)];
			expect(steps).toHaveLength(3);
			expect(steps[0].state).toEqual({ count: 1 });
			expect(steps[1].state).toEqual({ count: 2 });
			expect(steps[2].state).toEqual({ count: 3 });
		});

		it("cleans up on early break", () => {
			const projection = createProjection<{ count: number }>(counterSource);
			const events = [
				{
					eventType: "ItemAdded",
					streamId: "cart-1",
					sequenceNumber: 0,
					isJson: true,
					data: {},
				},
				{
					eventType: "ItemAdded",
					streamId: "cart-1",
					sequenceNumber: 1,
					isJson: true,
					data: {},
				},
				{
					eventType: "ItemAdded",
					streamId: "cart-1",
					sequenceNumber: 2,
					isJson: true,
					data: {},
				},
			];

			for (const step of projection.run(events)) {
				if (step.state!.count >= 1) break;
			}
		});
	});

	describe("run (async)", () => {
		it("iterates over async events", async () => {
			const projection = createProjection<{ count: number }>(counterSource);

			async function* events() {
				yield {
					eventType: "ItemAdded",
					streamId: "cart-1",
					sequenceNumber: 0,
					isJson: true,
					data: {},
				};
				yield {
					eventType: "ItemAdded",
					streamId: "cart-1",
					sequenceNumber: 1,
					isJson: true,
					data: {},
				};
			}

			const steps = [];
			for await (const step of projection.run(events())) {
				steps.push(step);
			}
			expect(steps).toHaveLength(2);
			expect(steps[1].state).toEqual({ count: 2 });
		});
	});

	describe("test", () => {
		it("creates a manual test session", () => {
			const projection = createProjection<{ count: number }>(counterSource);
			const test = projection.test();
			test.feed({
				eventType: "ItemAdded",
				streamId: "cart-1",
				sequenceNumber: 0,
				isJson: true,
				data: {},
			});
			expect(test.getState()).toEqual({ count: 1 });
			test.dispose();
		});
	});

	describe("systemEvents", () => {
		it("stream deletion feeds correctly", () => {
			const projection = createProjection<{ a: number; deleted?: boolean }>(`
				fromAll().foreachStream().when({
					$init: function() { return { a: 0 }; },
					ItemAdded: function(s, e) { s.a++; return s; },
					$deleted: function(s, e) { s.deleted = true; return s; }
				}).outputState()
			`);

			const test = projection.test();
			test.feed({
				eventType: "ItemAdded",
				streamId: "cart-123",
				sequenceNumber: 0,
				isJson: true,
				data: {},
			});
			test.feed(systemEvents.streamDeleted("cart-123", 1));
			expect(test.getState("cart-123")).toEqual({ a: 1, deleted: true });
			test.dispose();
		});
	});
});
