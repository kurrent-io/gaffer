import { describe, expect, it } from "vitest";
import { ProjectionTest } from "./ProjectionTest.js";
import {
	ProjectionError,
	ProjectionHandlerError,
} from "@kurrent/gaffer-runtime";

const counterSource = `
	fromAll().when({
		$init: function() { return { count: 0 }; },
		ItemAdded: function(s, e) { s.count++; return s; }
	})
`;

describe("ProjectionTest", () => {
	it("feeds events and returns state", () => {
		const test = new ProjectionTest<{ count: number }>(counterSource);
		const step = test.feed({
			eventType: "ItemAdded",
			streamId: "cart-1",
			sequenceNumber: 0,
			isJson: true,
			data: {},
		});
		expect(step.state).toEqual({ count: 1 });
		expect(step.result).toEqual({ count: 1 });
		expect(step.sharedState).toBeNull();
		expect(step.partition).toBeNull();
		expect(step.event).toEqual({
			eventType: "ItemAdded",
			streamId: "cart-1",
			sequenceNumber: 0,
			isJson: true,
			data: {},
		});
		test.dispose();
	});

	it("accumulates state across feeds", () => {
		const test = new ProjectionTest<{ count: number }>(counterSource);
		test.feed({
			eventType: "ItemAdded",
			streamId: "cart-1",
			sequenceNumber: 0,
			isJson: true,
			data: {},
		});
		test.feed({
			eventType: "ItemAdded",
			streamId: "cart-1",
			sequenceNumber: 1,
			isJson: true,
			data: {},
		});
		const step = test.feed({
			eventType: "ItemAdded",
			streamId: "cart-1",
			sequenceNumber: 2,
			isJson: true,
			data: {},
		});
		expect(step.state).toEqual({ count: 3 });
		test.dispose();
	});

	it("returns state by partition", () => {
		const test = new ProjectionTest<{ items: number }>(`
			fromCategory("cart").foreachStream().when({
				$init: function() { return { items: 0 }; },
				ItemAdded: function(s, e) { s.items++; return s; }
			})
		`);

		const step1 = test.feed({
			eventType: "ItemAdded",
			streamId: "cart-1",
			sequenceNumber: 0,
			isJson: true,
			data: {},
		});
		expect(step1.partition).toBe("cart-1");
		expect(step1.state).toEqual({ items: 1 });

		test.feed({
			eventType: "ItemAdded",
			streamId: "cart-1",
			sequenceNumber: 1,
			isJson: true,
			data: {},
		});

		const step3 = test.feed({
			eventType: "ItemAdded",
			streamId: "cart-2",
			sequenceNumber: 0,
			isJson: true,
			data: {},
		});
		expect(step3.partition).toBe("cart-2");
		expect(step3.state).toEqual({ items: 1 });

		expect(test.getState("cart-1")).toEqual({ items: 2 });
		expect(test.getState("cart-2")).toEqual({ items: 1 });
		test.dispose();
	});

	it("collects emitted events", () => {
		const test = new ProjectionTest(`
			fromAll().when({
				$init: function() { return {}; },
				OrderPlaced: function(s, e) {
					emit("notifications", "OrderNotification", { orderId: e.data.orderId });
					return s;
				}
			})
		`);

		const step = test.feed({
			eventType: "OrderPlaced",
			streamId: "order-1",
			sequenceNumber: 0,
			isJson: true,
			data: { orderId: "ABC" },
		});

		expect(step.emitted).toHaveLength(1);
		expect(step.emitted[0].streamId).toBe("notifications");
		expect(step.emitted[0].eventType).toBe("OrderNotification");
		expect(step.emitted[0].data).toEqual({ orderId: "ABC" });
		expect(step.emitted[0].isLink).toBe(false);
		test.dispose();
	});

	it("collects logs", () => {
		const test = new ProjectionTest(`
			fromAll().when({
				TestEvent: function(s, e) {
					log("hello from projection");
					return s;
				}
			})
		`);

		const step = test.feed({
			eventType: "TestEvent",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			data: {},
		});

		expect(step.logs).toEqual(["hello from projection"]);
		test.dispose();
	});

	it("resets emitted and logs per step", () => {
		const test = new ProjectionTest(`
			fromAll().when({
				$init: function() { return {}; },
				Ping: function(s, e) {
					log("ping");
					emit("out", "Pong", {});
					return s;
				}
			})
		`);

		const step1 = test.feed({
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			data: {},
		});
		expect(step1.logs).toHaveLength(1);
		expect(step1.emitted).toHaveLength(1);

		const step2 = test.feed({
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 1,
			isJson: true,
			data: {},
		});
		expect(step2.logs).toHaveLength(1);
		expect(step2.emitted).toHaveLength(1);
		test.dispose();
	});

	it("returns shared state for biState projections", () => {
		const test = new ProjectionTest<
			{ count: number },
			unknown,
			{ total: number }
		>(`
			options({ biState: true });
			fromAll().when({
				$init: function() { return { count: 0 }; },
				$initShared: function() { return { total: 0 }; },
				Added: function(s, e) {
					s[0].count++;
					s[1].total += e.data.amount;
					return s;
				}
			})
		`);

		test.feed({
			eventType: "Added",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			data: { amount: 10 },
		});
		const step = test.feed({
			eventType: "Added",
			streamId: "s-1",
			sequenceNumber: 1,
			isJson: true,
			data: { amount: 20 },
		});

		expect(step.state).toEqual({ count: 2 });
		expect(step.sharedState).toEqual({ total: 30 });
		expect(step.partition).toBeNull();
		test.dispose();
	});

	it("returns result with transformBy", () => {
		const test = new ProjectionTest<{ count: number }, { total: number }>(`
			fromAll().when({
				$init: function() { return { count: 0 }; },
				Ping: function(s, e) { s.count++; return s; }
			}).transformBy(function(s) {
				return { total: s.count * 2 };
			}).outputState()
		`);

		const step = test.feed({
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			data: {},
		});
		expect(step.result).toEqual({ total: 2 });
		test.dispose();
	});

	it("accepts object data and auto-stringifies", () => {
		const test = new ProjectionTest<{ total: number }>(`
			fromAll().when({
				$init: function() { return { total: 0 }; },
				Deposited: function(s, e) { s.total += e.data.amount; return s; }
			})
		`);

		test.feed({
			eventType: "Deposited",
			streamId: "acc-1",
			sequenceNumber: 0,
			isJson: true,
			data: { amount: 50 },
		});

		expect(test.getState()).toEqual({ total: 50 });
		test.dispose();
	});

	it("accepts RecordedEvent shape", () => {
		const test = new ProjectionTest<{ count: number }>(counterSource);
		test.feed({
			type: "ItemAdded",
			streamId: "cart-1",
			data: {},
			metadata: undefined,
			id: "00000000-0000-0000-0000-000000000000",
			isJson: true,
			revision: 0n,
			created: new Date(),
		});
		expect(test.getState()).toEqual({ count: 1 });
		test.dispose();
	});

	it("accepts ResolvedEvent shape", () => {
		const test = new ProjectionTest<{ count: number }>(counterSource);
		test.feed({
			event: {
				type: "ItemAdded",
				streamId: "cart-1",
				data: {},
				metadata: undefined,
				id: "00000000-0000-0000-0000-000000000000",
				isJson: true,
				revision: 0n,
				created: new Date(),
			},
		});
		expect(test.getState()).toEqual({ count: 1 });
		test.dispose();
	});

	it("wraps errors with event context", () => {
		const test = new ProjectionTest(`
			fromAll().when({
				$init: function() { return {}; },
				Bad: function(s, e) { throw "boom"; }
			})
		`);

		expect(() =>
			test.feed({
				eventType: "Bad",
				streamId: "s-1",
				sequenceNumber: 0,
				isJson: true,
				data: {},
			}),
		).toThrow(ProjectionHandlerError);

		expect.assertions(9);

		try {
			test.feed({
				eventType: "Bad",
				streamId: "s-1",
				sequenceNumber: 1,
				isJson: true,
				data: {},
			});
		} catch (err) {
			const e = err as ProjectionHandlerError;
			expect(e).toBeInstanceOf(ProjectionHandlerError);
			expect(e).toBeInstanceOf(ProjectionError);
			expect(e.event.eventType).toBe("Bad");
			expect(e.event.streamId).toBe("s-1");
			expect(e.event.sequenceNumber).toBe(1);
			expect(e.message).toContain("1@s-1");
			expect(e.message).toContain("Bad");
			expect(e.message).toContain("boom");
		}
		test.dispose();
	});

	it("unhandled event returns null state and partition", () => {
		const test = new ProjectionTest<{ count: number }>(counterSource);
		test.feed({
			eventType: "ItemAdded",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			data: {},
		});
		const step = test.feed({
			eventType: "UnknownEvent",
			streamId: "s-1",
			sequenceNumber: 1,
			isJson: true,
			data: {},
		});
		expect(step.state).toBeNull();
		expect(step.partition).toBeNull();
		test.dispose();
	});

	it("unhandled event in partitioned projection returns null state and partition", () => {
		const test = new ProjectionTest<{ items: number }>(`
			fromCategory("cart").foreachStream().when({
				$init: function() { return { items: 0 }; },
				ItemAdded: function(s, e) { s.items++; return s; }
			})
		`);

		test.feed({
			eventType: "ItemAdded",
			streamId: "cart-1",
			sequenceNumber: 0,
			isJson: true,
			data: {},
		});
		const step = test.feed({
			eventType: "UnknownEvent",
			streamId: "cart-1",
			sequenceNumber: 1,
			isJson: true,
			data: {},
		});
		expect(step.state).toBeNull();
		expect(step.partition).toBeNull();

		expect(test.getState("cart-1")).toEqual({ items: 1 });
		test.dispose();
	});

	it("handled event in unpartitioned projection returns state with null partition", () => {
		const test = new ProjectionTest<{ count: number }>(counterSource);
		const step = test.feed({
			eventType: "ItemAdded",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			data: {},
		});
		expect(step.state).toEqual({ count: 1 });
		expect(step.partition).toBeNull();
		test.dispose();
	});

	it("throws after dispose", () => {
		const test = new ProjectionTest(counterSource);
		test.dispose();
		expect(() =>
			test.feed({
				eventType: "ItemAdded",
				streamId: "s-1",
				sequenceNumber: 0,
				isJson: true,
				data: {},
			}),
		).toThrow("disposed");
		expect(() => test.getState()).toThrow("disposed");
		expect(() => test.getSharedState()).toThrow("disposed");
		expect(() => test.getResult()).toThrow("disposed");
	});

	it("double dispose is safe", () => {
		const test = new ProjectionTest(counterSource);
		test.dispose();
		test.dispose();
	});

	it("detects link events via isLink", () => {
		const test = new ProjectionTest(`
			fromAll().when({
				$init: function() { return {}; },
				OrderPlaced: function(s, e) {
					linkTo("all-orders", e);
					return s;
				}
			})
		`);

		const step = test.feed({
			eventType: "OrderPlaced",
			streamId: "order-1",
			sequenceNumber: 5,
			isJson: true,
			data: {},
		});

		expect(step.emitted).toHaveLength(1);
		expect(step.emitted[0].isLink).toBe(true);
		expect(step.emitted[0].eventType).toBe("$>");
		expect(step.emitted[0].data).toBe("5@order-1");
		test.dispose();
	});

	it("handles non-JSON emitted data gracefully", () => {
		const test = new ProjectionTest(`
			fromAll().when({
				$init: function() { return {}; },
				OrderPlaced: function(s, e) {
					linkTo("all-orders", e);
					return s;
				}
			})
		`);

		const step = test.feed({
			eventType: "OrderPlaced",
			streamId: "order-1",
			sequenceNumber: 3,
			isJson: true,
			data: {},
		});

		// linkTo data is "3@order-1" which is not valid JSON
		// mapEmittedEvent should fall back to raw string
		expect(step.emitted[0].data).toBe("3@order-1");
		test.dispose();
	});
});
