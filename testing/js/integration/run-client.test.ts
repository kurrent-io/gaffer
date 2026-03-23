import { describe, expect, it, beforeAll, afterAll } from "vitest";
import { KurrentDBClient, jsonEvent } from "@kurrent/kurrentdb-client";
import { createProjection } from "../src/createProjection.js";

const CONNECTION_STRING =
	process.env.KURRENTDB_URL ?? "kurrentdb://localhost:2113?tls=false";

describe("run(client)", () => {
	let client: KurrentDBClient;
	const suffix = Date.now();

	beforeAll(() => {
		client = KurrentDBClient.connectionString(CONNECTION_STRING);
	});

	afterAll(async () => {
		await client?.dispose();
	});

	it("fromAll - processes all events", async () => {
		const stream = `all-test-${suffix}`;

		await client.appendToStream(stream, [
			jsonEvent({ type: "Ping", data: {} }),
			jsonEvent({ type: "Ping", data: {} }),
			jsonEvent({ type: "Ping", data: {} }),
		]);

		const projection = createProjection<{ count: number }>(`
			fromAll().when({
				$init: function() { return { count: 0 }; },
				Ping: function(s, e) { s.count++; return s; }
			})
		`);

		const steps = [];
		for await (const step of projection.run(client)) {
			steps.push(step);
			if (steps.length >= 3) break;
		}

		expect(steps).toHaveLength(3);
		expect(steps[2].state).toEqual({ count: 3 });
	});

	it("fromCategory - processes events from matching streams", async () => {
		const stream1 = `orders-${suffix}-1`;
		const stream2 = `orders-${suffix}-2`;

		await client.appendToStream(stream1, [
			jsonEvent({ type: "OrderPlaced", data: { amount: 10 } }),
		]);
		await client.appendToStream(stream2, [
			jsonEvent({ type: "OrderPlaced", data: { amount: 20 } }),
		]);

		const projection = createProjection<{ total: number }>(`
			fromCategory("orders").when({
				$init: function() { return { total: 0 }; },
				OrderPlaced: function(s, e) { s.total += e.data.amount; return s; }
			})
		`);

		const steps = [];
		for await (const step of projection.run(client)) {
			steps.push(step);
			if (steps.length >= 2) break;
		}

		expect(steps).toHaveLength(2);
		expect(steps[1].state!.total).toBe(30);
	});

	it("fromStream - processes events from a single stream", async () => {
		const stream = `single-stream-${suffix}`;

		await client.appendToStream(stream, [
			jsonEvent({ type: "Tick", data: {} }),
			jsonEvent({ type: "Tick", data: {} }),
		]);

		const projection = createProjection<{ count: number }>(`
			fromStream("${stream}").when({
				$init: function() { return { count: 0 }; },
				Tick: function(s, e) { s.count++; return s; }
			})
		`);

		const steps = [];
		for await (const step of projection.run(client)) {
			steps.push(step);
			if (steps.length >= 2) break;
		}

		expect(steps).toHaveLength(2);
		expect(steps[1].state).toEqual({ count: 2 });
	});

	// fromStreams segfaults the NativeAOT runtime (tracked in testing/todo.md)
	it.skip("fromStreams - processes events from specific streams", async () => {
		const streamA = `specific-a-${suffix}`;
		const streamB = `specific-b-${suffix}`;

		await client.appendToStream(streamA, [
			jsonEvent({ type: "Hit", data: {} }),
		]);
		await client.appendToStream(streamB, [
			jsonEvent({ type: "Hit", data: {} }),
		]);

		const source = [
			`fromStreams("${streamA}", "${streamB}")`,
			".when({",
			"  $init: function() { return { count: 0 }; },",
			"  Hit: function(s, e) { s.count++; return s; }",
			"})",
		].join("\n");

		const projection = createProjection<{ count: number }>(source);

		const steps = [];
		for await (const step of projection.run(client)) {
			steps.push(step);
			if (steps.length >= 2) break;
		}

		expect(steps).toHaveLength(2);
		expect(steps[1].state).toEqual({ count: 2 });
	});

	it("foreachStream - partitions state by stream", async () => {
		const stream1 = `carts-${suffix}-1`;
		const stream2 = `carts-${suffix}-2`;

		await client.appendToStream(stream1, [
			jsonEvent({ type: "ItemAdded", data: {} }),
			jsonEvent({ type: "ItemAdded", data: {} }),
		]);
		await client.appendToStream(stream2, [
			jsonEvent({ type: "ItemAdded", data: {} }),
		]);

		const projection = createProjection<{ items: number }>(`
			fromCategory("carts").foreachStream().when({
				$init: function() { return { items: 0 }; },
				ItemAdded: function(s, e) { s.items++; return s; }
			})
		`);

		const steps = [];
		for await (const step of projection.run(client)) {
			steps.push(step);
			if (steps.length >= 3) break;
		}

		expect(steps).toHaveLength(3);
	});

	it("collects emitted events in step results", async () => {
		const stream = `emit-test-${suffix}`;
		const eventType = `EmitTest_${suffix}`;

		await client.appendToStream(stream, [
			jsonEvent({ type: eventType, data: { orderId: "ABC" } }),
		]);

		const projection = createProjection(`
			fromAll().when({
				$init: function() { return {}; },
				${eventType}: function(s, e) {
					emit("notifications", "OrderNotification", { orderId: e.data.orderId });
					return s;
				}
			})
		`);

		for await (const step of projection.run(client)) {
			if (step.emitted.length > 0) {
				expect(step.emitted[0].streamId).toBe("notifications");
				expect(step.emitted[0].eventType).toBe("OrderNotification");
				expect(step.emitted[0].data).toEqual({ orderId: "ABC" });
				break;
			}
		}
	});

	it("collects log output in step results", async () => {
		const stream = `log-test-${suffix}`;
		const eventType = `LogTest_${suffix}`;

		await client.appendToStream(stream, [
			jsonEvent({ type: eventType, data: {} }),
		]);

		const projection = createProjection(`
			fromAll().when({
				${eventType}: function(s, e) {
					log("hello from integration");
					return s;
				}
			})
		`);

		for await (const step of projection.run(client)) {
			if (step.logs.length > 0) {
				expect(step.logs).toContain("hello from integration");
				break;
			}
		}
	});

	it("accesses event data from real events", async () => {
		const stream = `data-test-${suffix}`;
		const eventType = `DataTest_${suffix}`;

		await client.appendToStream(stream, [
			jsonEvent({ type: eventType, data: { amount: 50 } }),
			jsonEvent({ type: eventType, data: { amount: 30 } }),
		]);

		const projection = createProjection<{ total: number }>(`
			fromAll().when({
				$init: function() { return { total: 0 }; },
				${eventType}: function(s, e) { s.total += e.data.amount; return s; }
			})
		`);

		const steps = [];
		for await (const step of projection.run(client)) {
			steps.push(step);
			if (steps.length >= 2) break;
		}

		expect(steps[1].state).toEqual({ total: 80 });
	});
});
