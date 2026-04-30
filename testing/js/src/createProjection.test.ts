import { describe, expect, it } from "vitest";
import { createProjection } from "./createProjection.js";
import {
	InvalidProjectionError,
	CompilationTimeoutError,
} from "@kurrent/gaffer-runtime";
import { systemEvents } from "./systemEvents.js";

const counterSource = `
	fromAll().when({
		$init() { return { count: 0 }; },
		ItemAdded(s, e) { s.count++; return s; }
	})
`;

describe("createProjection", () => {
	describe("validate", () => {
		it("returns info for good projection", () => {
			const projection = createProjection(counterSource, { engineVersion: 2 });
			const info = projection.validate();
			expect(info.source.type).toBe("all");
			expect(info.partitioning.type).toBe("none");
			expect(info.events).toContain("ItemAdded");
		});

		it("throws for bad JS", () => {
			const projection = createProjection("this is not valid {{{{", {
				engineVersion: 2,
			});
			expect(() => projection.validate()).toThrow(InvalidProjectionError);
		});

		it("maps foreachStream partitioning", () => {
			const projection = createProjection(
				`
				fromAll().foreachStream().when({
					$init() { return {}; },
					Ping(s, e) { return s; }
				})
			`,
				{ engineVersion: 2 },
			);
			expect(projection.validate().partitioning.type).toBe("byStream");
		});

		it("maps category source", () => {
			const projection = createProjection(
				`
				fromCategory("orders").when({
					$init() { return {}; },
					Ping(s, e) { return s; }
				})
			`,
				{ engineVersion: 2 },
			);
			expect(projection.validate().source).toEqual({
				type: "categories",
				categories: ["orders"],
			});
		});

		it("maps biState", () => {
			const projection = createProjection(
				`
				options({ biState: true });
				fromAll().when({
					$init() { return {}; },
					$initShared() { return {}; },
					Ping(s, e) { return s; }
				})
			`,
				{ engineVersion: 2 },
			);
			expect(projection.validate().biState).toBe(true);
		});
	});

	describe("run (invalid)", () => {
		it("throws on invalid projection", () => {
			const projection = createProjection("this is not valid {{{{", {
				engineVersion: 2,
			});
			expect(() => [...projection.run([])]).toThrow(InvalidProjectionError);
		});

		it("throws on invalid input type", () => {
			const projection = createProjection(counterSource, { engineVersion: 2 });
			expect(() => [
				...projection.run(42 as unknown as Iterable<never>),
			]).toThrow("run() expects");
		});

		it("propagates handler error mid-iteration", () => {
			const source = `
				fromAll().when({
					$init() { return { count: 0 }; },
					Good(s, e) { s.count++; return s; },
					Bad(s, e) { throw "mid-stream error"; }
				})
			`;
			const projection = createProjection<{ count: number }>(source, {
				engineVersion: 2,
			});
			const events = [
				{
					eventType: "Good",
					streamId: "s-1",
					sequenceNumber: 0,
					isJson: true,
					data: {},
				},
				{
					eventType: "Bad",
					streamId: "s-1",
					sequenceNumber: 1,
					isJson: true,
					data: {},
				},
				{
					eventType: "Good",
					streamId: "s-1",
					sequenceNumber: 2,
					isJson: true,
					data: {},
				},
			];

			const results: Array<{ count: number } | null> = [];
			expect(() => {
				for (const step of projection.run(events)) {
					if (step.status === "skipped") continue;
					results.push(step.state);
				}
			}).toThrow("mid-stream error");
			expect(results).toHaveLength(1);
			expect(results[0]?.count).toBe(1);
		});
	});

	describe("options", () => {
		it("passes compilation timeout to session", () => {
			const projection = createProjection("while(true) {}", {
				engineVersion: 2,
				databaseConfig: { compilationTimeoutMs: 100 },
			});
			expect(() => [...projection.run([])]).toThrow(CompilationTimeoutError);
		});

		it("v1 drops non-JSON events", () => {
			const projection = createProjection<{ count: number }>(counterSource, {
				engineVersion: 1,
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
			expect(results[0].status).toBe("skipped");
			expect(results[1].status).toBe("processed");
			if (results[1].status === "processed") {
				expect(results[1].state?.count).toBe(1);
			}
		});
	});

	describe("run (empty)", () => {
		it("yields no results for empty iterable", () => {
			const projection = createProjection<{ count: number }>(counterSource, {
				engineVersion: 2,
			});
			const results = [...projection.run([])];
			expect(results).toHaveLength(0);
		});
	});

	describe("run (sync)", () => {
		it("iterates over events and yields state", () => {
			const projection = createProjection<{ count: number }>(counterSource, {
				engineVersion: 2,
			});
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

			const states: Array<{ count: number }> = [];
			for (const step of projection.run(events)) {
				if (step.status === "skipped") continue;
				states.push(step.state);
			}
			expect(states).toHaveLength(3);
			expect(states[0]).toEqual({ count: 1 });
			expect(states[1]).toEqual({ count: 2 });
			expect(states[2]).toEqual({ count: 3 });
		});

		it("cleans up on early break", () => {
			const projection = createProjection<{ count: number }>(counterSource, {
				engineVersion: 2,
			});
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
				if (step.status === "skipped") continue;
				if (step.state!.count >= 1) break;
			}
		});
	});

	describe("run (async)", () => {
		it("iterates over async events", async () => {
			const projection = createProjection<{ count: number }>(counterSource, {
				engineVersion: 2,
			});

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

			const states: Array<{ count: number }> = [];
			for await (const step of projection.run(events())) {
				if (step.status === "skipped") continue;
				states.push(step.state);
			}
			expect(states).toHaveLength(2);
			expect(states[1]).toEqual({ count: 2 });
		});

		it("propagates handler error mid-async-stream", async () => {
			const source = `
				fromAll().when({
					$init() { return { count: 0 }; },
					Good(s, e) { s.count++; return s; },
					Bad(s, e) { throw "async error"; }
				})
			`;
			const projection = createProjection<{ count: number }>(source, {
				engineVersion: 2,
			});

			async function* events() {
				yield {
					eventType: "Good",
					streamId: "s-1",
					sequenceNumber: 0,
					isJson: true,
					data: {},
				};
				yield {
					eventType: "Bad",
					streamId: "s-1",
					sequenceNumber: 1,
					isJson: true,
					data: {},
				};
			}

			const results: Array<{ count: number } | null> = [];
			await expect(async () => {
				for await (const step of projection.run(events())) {
					if (step.status === "skipped") continue;
					results.push(step.state);
				}
			}).rejects.toThrow("async error");
			expect(results).toHaveLength(1);
		});
	});

	describe("test", () => {
		it("creates a manual test session", () => {
			const projection = createProjection<{ count: number }>(counterSource, {
				engineVersion: 2,
			});
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
			const projection = createProjection<{ a: number; deleted?: boolean }>(
				`
				fromAll().foreachStream().when({
					$init() { return { a: 0 }; },
					ItemAdded(s, e) { s.a++; return s; },
					$deleted(s, e) { s.deleted = true; return s; }
				}).outputState()
			`,
				{ engineVersion: 2 },
			);

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
