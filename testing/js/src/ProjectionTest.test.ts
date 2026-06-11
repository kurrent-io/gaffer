import { describe, expect, it } from "vitest";
import { ProjectionTest, toSessionOptions } from "./ProjectionTest.js";
import { systemEvents } from "./systemEvents.js";
import {
	InvalidArgumentError,
	ProjectionError,
	ProjectionHandlerError,
} from "@kurrent/gaffer-runtime";

const counterSource = `
	fromAll().when({
		$init() { return { count: 0 }; },
		ItemAdded(s, e) { s.count++; return s; }
	})
`;

describe("ProjectionTest", () => {
	it("feeds events and returns state", () => {
		const test = new ProjectionTest<{ count: number }>(counterSource, {
			engineVersion: 2,
		});
		const step = test.feed({
			eventType: "ItemAdded",
			streamId: "cart-1",
			sequenceNumber: 0,
			isJson: true,
			data: {},
		});
		expect(step.status).toBe("processed");
		if (step.status !== "processed") return;
		expect(step.state).toEqual({ count: 1 });
		expect(step.result).toEqual({ count: 1 });
		expect(step.sharedState).toBeUndefined();
		expect(step.partition).toBeUndefined();
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
		const test = new ProjectionTest<{ count: number }>(counterSource, {
			engineVersion: 2,
		});
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
		expect(step.status).toBe("processed");
		if (step.status !== "processed") return;
		expect(step.state).toEqual({ count: 3 });
		test.dispose();
	});

	it("returns state by partition", () => {
		const test = new ProjectionTest<{ items: number }>(
			`
			fromCategory("cart").foreachStream().when({
				$init() { return { items: 0 }; },
				ItemAdded(s, e) { s.items++; return s; }
			})
		`,
			{ engineVersion: 2 },
		);

		const step1 = test.feed({
			eventType: "ItemAdded",
			streamId: "cart-1",
			sequenceNumber: 0,
			isJson: true,
			data: {},
		});
		expect(step1.status).toBe("processed");
		if (step1.status !== "processed") return;
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
		expect(step3.status).toBe("processed");
		if (step3.status !== "processed") return;
		expect(step3.partition).toBe("cart-2");
		expect(step3.state).toEqual({ items: 1 });

		expect(test.getState("cart-1")).toEqual({ items: 2 });
		expect(test.getState("cart-2")).toEqual({ items: 1 });
		test.dispose();
	});

	it("collects emitted events", () => {
		const test = new ProjectionTest(
			`
			fromAll().when({
				$init() { return {}; },
				OrderPlaced(s, e) {
					emit("notifications", "OrderNotification", { orderId: e.data.orderId });
					return s;
				}
			})
		`,
			{ engineVersion: 2 },
		);

		const step = test.feed({
			eventType: "OrderPlaced",
			streamId: "order-1",
			sequenceNumber: 0,
			isJson: true,
			data: { orderId: "ABC" },
		});

		expect(step.status).toBe("processed");
		if (step.status !== "processed") return;
		expect(step.emitted).toHaveLength(1);
		expect(step.emitted[0]?.streamId).toBe("notifications");
		expect(step.emitted[0]?.eventType).toBe("OrderNotification");
		expect(step.emitted[0]?.data).toEqual({ orderId: "ABC" });
		expect(step.emitted[0]?.isLink).toBe(false);
		test.dispose();
	});

	it("collects logs", () => {
		const test = new ProjectionTest(
			`
			fromAll().when({
				TestEvent(s, e) {
					log("hello from projection");
					return s;
				}
			})
		`,
			{ engineVersion: 2 },
		);

		const step = test.feed({
			eventType: "TestEvent",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			data: {},
		});

		expect(step.status).toBe("processed");
		if (step.status !== "processed") return;
		expect(step.logs).toEqual(["hello from projection"]);
		test.dispose();
	});

	it("resets emitted and logs per step", () => {
		const test = new ProjectionTest(
			`
			fromAll().when({
				$init() { return {}; },
				Ping(s, e) {
					log("ping");
					emit("out", "Pong", {});
					return s;
				}
			})
		`,
			{ engineVersion: 2 },
		);

		const step1 = test.feed({
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			data: {},
		});
		expect(step1.status).toBe("processed");
		if (step1.status !== "processed") return;
		expect(step1.logs).toHaveLength(1);
		expect(step1.emitted).toHaveLength(1);

		const step2 = test.feed({
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 1,
			isJson: true,
			data: {},
		});
		expect(step2.status).toBe("processed");
		if (step2.status !== "processed") return;
		expect(step2.logs).toHaveLength(1);
		expect(step2.emitted).toHaveLength(1);
		test.dispose();
	});

	it("returns shared state for biState projections", () => {
		const test = new ProjectionTest<
			{ count: number },
			unknown,
			{ total: number }
		>(
			`
			options({ biState: true });
			fromAll().when({
				$init() { return { count: 0 }; },
				$initShared() { return { total: 0 }; },
				Added(s, e) {
					s[0].count++;
					s[1].total += e.data.amount;
					return s;
				}
			})
		`,
			{ engineVersion: 2 },
		);

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

		expect(step.status).toBe("processed");
		if (step.status !== "processed") return;
		expect(step.state).toEqual({ count: 2 });
		expect(step.sharedState).toEqual({ total: 30 });
		expect(step.partition).toBeUndefined();
		test.dispose();
	});

	it("returns result with transformBy", () => {
		// V1 only - V2 doesn't iterate transforms.
		const test = new ProjectionTest<{ count: number }, { total: number }>(
			`
			fromAll().when({
				$init() { return { count: 0 }; },
				Ping(s, e) { s.count++; return s; }
			}).transformBy(function(s) {
				return { total: s.count * 2 };
			}).outputState()
		`,
			{ engineVersion: 1 },
		);

		const step = test.feed({
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			data: {},
		});
		expect(step.status).toBe("processed");
		if (step.status !== "processed") return;
		expect(step.result).toEqual({ total: 2 });
		test.dispose();
	});

	it("accepts object data and auto-stringifies", () => {
		const test = new ProjectionTest<{ total: number }>(
			`
			fromAll().when({
				$init() { return { total: 0 }; },
				Deposited(s, e) { s.total += e.data.amount; return s; }
			})
		`,
			{ engineVersion: 2 },
		);

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
		const test = new ProjectionTest<{ count: number }>(counterSource, {
			engineVersion: 2,
		});
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
		const test = new ProjectionTest<{ count: number }>(counterSource, {
			engineVersion: 2,
		});
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
		const test = new ProjectionTest(
			`
			fromAll().when({
				$init() { return {}; },
				Bad(s, e) { throw "boom"; }
			})
		`,
			{ engineVersion: 2 },
		);

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

	it("unhandled event returns skipped result", () => {
		const test = new ProjectionTest<{ count: number }>(counterSource, {
			engineVersion: 2,
		});
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
		expect(step.status).toBe("skipped");
		if (step.status !== "skipped") return;
		expect(step.reason).toBe("unhandled");
		test.dispose();
	});

	it("unhandled event in partitioned projection returns skipped", () => {
		const test = new ProjectionTest<{ items: number }>(
			`
			fromCategory("cart").foreachStream().when({
				$init() { return { items: 0 }; },
				ItemAdded(s, e) { s.items++; return s; }
			})
		`,
			{ engineVersion: 2 },
		);

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
		expect(step.status).toBe("skipped");
		if (step.status === "skipped") {
			expect(step.reason).toBe("unhandled");
		}

		expect(test.getState("cart-1")).toEqual({ items: 1 });
		test.dispose();
	});

	it("handled event in unpartitioned projection has no partition", () => {
		const test = new ProjectionTest<{ count: number }>(counterSource, {
			engineVersion: 2,
		});
		const step = test.feed({
			eventType: "ItemAdded",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			data: {},
		});
		expect(step.status).toBe("processed");
		if (step.status !== "processed") return;
		expect(step.state).toEqual({ count: 1 });
		expect(step.partition).toBeUndefined();
		test.dispose();
	});

	it("throws after dispose", () => {
		const test = new ProjectionTest(counterSource, { engineVersion: 2 });
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
		expect(() => test.getStateRaw()).toThrow("disposed");
		expect(() => test.getSharedState()).toThrow("disposed");
		expect(() => test.getResult()).toThrow("disposed");
	});

	it("double dispose is safe", () => {
		const test = new ProjectionTest(counterSource, { engineVersion: 2 });
		test.dispose();
		test.dispose();
	});

	it("detects link events via isLink", () => {
		const test = new ProjectionTest(
			`
			fromAll().when({
				$init() { return {}; },
				OrderPlaced(s, e) {
					linkTo("all-orders", e);
					return s;
				}
			})
		`,
			{ engineVersion: 2 },
		);

		const step = test.feed({
			eventType: "OrderPlaced",
			streamId: "order-1",
			sequenceNumber: 5,
			isJson: true,
			data: {},
		});

		expect(step.status).toBe("processed");
		if (step.status !== "processed") return;
		expect(step.emitted).toHaveLength(1);
		expect(step.emitted[0]?.isLink).toBe(true);
		expect(step.emitted[0]?.eventType).toBe("$>");
		expect(step.emitted[0]?.data).toBe("5@order-1");
		test.dispose();
	});

	it("handles non-JSON emitted data gracefully", () => {
		const test = new ProjectionTest(
			`
			fromAll().when({
				$init() { return {}; },
				OrderPlaced(s, e) {
					linkTo("all-orders", e);
					return s;
				}
			})
		`,
			{ engineVersion: 2 },
		);

		const step = test.feed({
			eventType: "OrderPlaced",
			streamId: "order-1",
			sequenceNumber: 3,
			isJson: true,
			data: {},
		});

		expect(step.status).toBe("processed");
		if (step.status !== "processed") return;
		expect(step.emitted[0]?.data).toBe("3@order-1");
		test.dispose();
	});

	it("passes quirksVersion through to the runtime session", () => {
		const test = new ProjectionTest(
			"fromAll().when({ $any: function (s, e) { return s; } });",
			{ engineVersion: 2, quirksVersion: "26.1.0" },
		);
		test.dispose();
	});

	it("rejects malformed quirksVersion at construction", () => {
		// Validation lives in the runtime; we verify the typed error
		// surfaces unwrapped through the testing-lib boundary.
		expect(
			() =>
				new ProjectionTest("fromAll()", {
					engineVersion: 2,
					quirksVersion: "not-a-version",
				}),
		).toThrow(InvalidArgumentError);
		try {
			new ProjectionTest("fromAll()", {
				engineVersion: 2,
				quirksVersion: "not-a-version",
			});
		} catch (err) {
			expect(err).toBeInstanceOf(InvalidArgumentError);
			expect((err as InvalidArgumentError).field).toBe("quirksVersion");
		}
	});
});

describe("toSessionOptions", () => {
	it("includes quirksVersion when set", () => {
		const out = toSessionOptions({ engineVersion: 2, quirksVersion: "26.1.0" });
		expect(out.quirksVersion).toBe("26.1.0");
	});

	it("omits quirksVersion when unset", () => {
		const out = toSessionOptions({ engineVersion: 2 });
		expect(out.quirksVersion).toBeUndefined();
	});

	it("quirksVersion is a sibling, not under databaseConfig", () => {
		// The mental model: engineVersion + quirksVersion are "what target am
		// I matching"; databaseConfig is for runtime knobs (timeouts).
		const out = toSessionOptions({
			engineVersion: 2,
			quirksVersion: "26.1.0",
			databaseConfig: { compilationTimeoutMs: 1000 },
		});
		expect(out.quirksVersion).toBe("26.1.0");
		expect(out.compilationTimeoutMs).toBe(1000);
	});

	it("maxStateSizeBytes forwards from databaseConfig", () => {
		const out = toSessionOptions({
			engineVersion: 2,
			databaseConfig: { maxStateSizeBytes: 1024 },
		});
		expect(out.maxStateSizeBytes).toBe(1024);
	});
});

describe("source filtering", () => {
	const fromStreamSource = `
		fromStream("s-1").when({
			$init() { return { count: 0 }; },
			Ping(s, e) { s.count++; return s; }
		})
	`;

	it("processes events on the declared stream", () => {
		using test = new ProjectionTest<{ count: number }>(fromStreamSource, {
			engineVersion: 2,
		});
		const step = test.feed({
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			data: {},
		});
		expect(step.status).toBe("processed");
	});

	it("skips events on a different stream with reason wrong-stream", () => {
		using test = new ProjectionTest<{ count: number }>(fromStreamSource, {
			engineVersion: 2,
		});
		const step = test.feed({
			eventType: "Ping",
			streamId: "s-2",
			sequenceNumber: 0,
			isJson: true,
			data: {},
		});
		expect(step.status).toBe("skipped");
		if (step.status !== "skipped") return;
		expect(step.reason).toBe("wrong-stream");
		// The event never reached the handler.
		expect(test.getState("s-1")).toBeNull();
	});

	it("matches fromCategory by stream prefix", () => {
		using test = new ProjectionTest<{ count: number }>(
			`
			fromCategory("cart").when({
				$init() { return { count: 0 }; },
				Ping(s, e) { s.count++; return s; }
			})
		`,
			{ engineVersion: 2 },
		);
		expect(
			test.feed({
				eventType: "Ping",
				streamId: "cart-1",
				sequenceNumber: 0,
				isJson: true,
				data: {},
			}).status,
		).toBe("processed");

		const off = test.feed({
			eventType: "Ping",
			streamId: "order-1",
			sequenceNumber: 0,
			isJson: true,
			data: {},
		});
		expect(off.status).toBe("skipped");
		if (off.status !== "skipped") return;
		expect(off.reason).toBe("wrong-stream");
	});

	it("skips an off-source $streamDeleted with reason wrong-stream", () => {
		using test = new ProjectionTest<{ count: number }>(fromStreamSource, {
			engineVersion: 2,
		});
		const step = test.feed(systemEvents.streamDeleted("s-2", 0));
		expect(step.status).toBe("skipped");
		if (step.status !== "skipped") return;
		expect(step.reason).toBe("wrong-stream");
	});

	it("fromAll never skips for wrong-stream", () => {
		using test = new ProjectionTest<{ count: number }>(counterSource, {
			engineVersion: 2,
		});
		const step = test.feed({
			eventType: "ItemAdded",
			streamId: "literally-anything",
			sequenceNumber: 0,
			isJson: true,
			data: {},
		});
		expect(step.status).toBe("processed");
	});
});

describe("runtime diagnostics (uni-state raw string)", () => {
	const rawStringState = `
		fromAll().when({
			Set: function (s, e) { return e.data.name; }
		});
	`;

	it("surfaces the raw un-encoded state and a diagnostic", () => {
		using test = new ProjectionTest(rawStringState, { engineVersion: 2 });
		const step = test.feed({
			eventType: "Set",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			data: { name: "alice" },
		});
		expect(step.status).toBe("processed");
		if (step.status !== "processed") return;

		// gaffer JSON-encodes the state (safe) and flags the quirk: pre-26.2.0 the engine
		// would persist a bare string raw and fault on reload.
		expect(step.state).toBe("alice");
		expect(step.stateRaw).toBe('"alice"');

		// The diagnostic tells the user to look, without them knowing in advance.
		expect(
			step.diagnostics.some((d) => d.code === "quirk.serialize.rawString"),
		).toBe(true);
	});

	it("carries diagnostics on a throwing quirk's error", () => {
		// A quirk that throws (NaN state -> serialize.nonFinite) reaches the
		// diagnostics channel on the error, not just a compatCode.
		using test = new ProjectionTest(
			`fromAll().when({
				$init: function () { return {}; },
				Bad: function (s, e) { s.v = NaN; return s; }
			});`,
			{ engineVersion: 2 },
		);

		let caught: unknown;
		try {
			test.feed({
				eventType: "Bad",
				streamId: "s-1",
				sequenceNumber: 0,
				isJson: true,
				data: {},
			});
		} catch (err) {
			caught = err;
		}

		expect(caught).toBeInstanceOf(ProjectionError);
		const diagnostics = (caught as ProjectionError).diagnostics ?? [];
		expect(
			diagnostics.some((d) => d.code === "quirk.serialize.nonFinite"),
		).toBe(true);
	});

	it("emits no diagnostic when slots hold objects", () => {
		using test = new ProjectionTest(
			`
			options({ biState: true });
			fromAll().when({
				$init: function () { return {}; },
				$initShared: function () { return {}; },
				SetName: function (s, e) { s[0] = { name: e.data.name }; return s; }
			});
		`,
			{ engineVersion: 2 },
		);
		const step = test.feed({
			eventType: "SetName",
			streamId: "s-1",
			sequenceNumber: 0,
			isJson: true,
			data: { name: "alice" },
		});
		expect(step.status).toBe("processed");
		if (step.status !== "processed") return;
		expect(step.diagnostics).toEqual([]);
	});
});
