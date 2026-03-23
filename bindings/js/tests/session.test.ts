import { describe, it, expect, afterEach } from "vitest";
import { ProjectionSession } from "../src/index.js";
import type { EmittedEvent } from "../src/index.js";

describe("ProjectionSession", () => {
	let session: ProjectionSession | null = null;

	afterEach(() => {
		session?.dispose();
		session = null;
	});

	it("creates and destroys a session", () => {
		session = new ProjectionSession(`
			fromAll().when({
				$init: function() { return {}; },
				Ping: function(s, e) { return s; }
			})
		`);
	});

	it("throws on invalid JS", () => {
		expect(() => new ProjectionSession("this is not valid {{{{")).toThrow(
			/Failed to create projection session/,
		);
	});

	it("feeds events and gets state", () => {
		session = new ProjectionSession(`
			fromAll().when({
				$init: function() { return { count: 0 }; },
				ItemAdded: function(s, e) { s.count++; return s; }
			})
		`);

		session.feed({ eventType: "ItemAdded", streamId: "cart-1", data: "{}" });
		session.feed({ eventType: "ItemAdded", streamId: "cart-1", data: "{}" });
		session.feed({ eventType: "ItemAdded", streamId: "cart-1", data: "{}" });

		expect(session.getStateJson<{ count: number }>()?.count).toBe(3);
	});

	it("accesses event data", () => {
		session = new ProjectionSession(`
			fromAll().when({
				$init: function() { return { total: 0 }; },
				Deposited: function(s, e) { s.total += e.data.amount; return s; }
			})
		`);

		session.feed({
			eventType: "Deposited",
			streamId: "acc-1",
			data: '{"amount":50}',
		});
		session.feed({
			eventType: "Deposited",
			streamId: "acc-1",
			data: '{"amount":30}',
		});

		expect(session.getStateJson<{ total: number }>()?.total).toBe(80);
	});

	it("partitions by stream", () => {
		session = new ProjectionSession(`
			fromCategory("cart").foreachStream().when({
				$init: function() { return { items: 0 }; },
				ItemAdded: function(s, e) { s.items++; return s; }
			})
		`);

		session.feed({ eventType: "ItemAdded", streamId: "cart-1", data: "{}" });
		session.feed({ eventType: "ItemAdded", streamId: "cart-1", data: "{}" });
		session.feed({ eventType: "ItemAdded", streamId: "cart-2", data: "{}" });

		expect(session.getStateJson<{ items: number }>("cart-1")?.items).toBe(2);
		expect(session.getStateJson<{ items: number }>("cart-2")?.items).toBe(1);
	});

	it("gets source definition", () => {
		session = new ProjectionSession(`
			fromAll().foreachStream().when({
				$init: function() { return {}; },
				Ping: function(s, e) { return s; }
			})
		`);

		const sources = session.getSources();
		expect(sources.AllStreams).toBe(true);
		expect(sources.ByStreams).toBe(true);
	});

	it("sets and restores state", () => {
		session = new ProjectionSession(`
			fromAll().when({
				$init: function() { return { count: 0 }; },
				Ping: function(s, e) { s.count++; return s; }
			})
		`);

		session.setState(null, '{"count":10}');
		session.feed({ eventType: "Ping", streamId: "s-1", data: "{}" });

		expect(session.getStateJson<{ count: number }>()?.count).toBe(11);
	});

	it("throws on handler error", () => {
		session = new ProjectionSession(`
			fromAll().when({
				$init: function() { return {}; },
				Bad: function(s, e) { throw "boom"; }
			})
		`);

		expect(() =>
			session!.feed({ eventType: "Bad", streamId: "s-1", data: "{}" }),
		).toThrow(/boom/);
	});

	it("returns null for unknown partition", () => {
		session = new ProjectionSession(`
			fromAll().foreachStream().when({
				$init: function() { return {}; },
				Ping: function(s, e) { return s; }
			})
		`);

		expect(session.getState("nonexistent")).toBeNull();
	});

	it("throws after dispose", () => {
		const s = new ProjectionSession(`
			fromAll().when({
				$init: function() { return {}; },
				Ping: function(s, e) { return s; }
			})
		`);
		s.dispose();

		expect(() =>
			s.feed({ eventType: "Ping", streamId: "s-1", data: "{}" }),
		).toThrow(/disposed/);
	});

	it("double dispose is safe", () => {
		const s = new ProjectionSession(`
			fromAll().when({
				$init: function() { return {}; },
				Ping: function(s, e) { return s; }
			})
		`);
		s.dispose();
		s.dispose(); // should not throw
	});

	it("onEmit receives emitted events", () => {
		session = new ProjectionSession(`
			fromAll().when({
				$init: function() { return {}; },
				OrderPlaced: function(s, e) {
					emit("notifications", "OrderNotification", { orderId: e.data.orderId });
					return s;
				}
			})
		`);

		const emitted: EmittedEvent[] = [];
		session.onEmit((e) => emitted.push(e));

		session.feed({
			eventType: "OrderPlaced",
			streamId: "order-1",
			data: '{"orderId":"ABC"}',
		});

		expect(emitted).toHaveLength(1);
		expect(emitted[0].streamId).toBe("notifications");
		expect(emitted[0].eventType).toBe("OrderNotification");
		expect(emitted[0].data).toContain("ABC");
	});

	it("onLog captures console.log", () => {
		session = new ProjectionSession(`
			fromAll().when({
				TestEvent: function(s, e) {
					log("hello from projection");
					return s;
				}
			})
		`);

		const logs: string[] = [];
		session.onLog((msg) => logs.push(msg));

		session.feed({ eventType: "TestEvent", streamId: "s-1", data: "{}" });

		expect(logs).toHaveLength(1);
		expect(logs[0]).toBe("hello from projection");
	});

	it("onStateChanged fires on state update", () => {
		session = new ProjectionSession(`
			fromAll().when({
				$init: function() { return { count: 0 }; },
				Ping: function(s, e) { s.count++; return s; }
			})
		`);

		const changes: Array<{ partition: string; state: string | null }> = [];
		session.onStateChanged((partition, state) =>
			changes.push({ partition, state }),
		);

		session.feed({ eventType: "Ping", streamId: "s-1", data: "{}" });
		session.feed({ eventType: "Ping", streamId: "s-1", data: "{}" });

		expect(changes).toHaveLength(2);
		expect(changes[0].state).toContain('"count":1');
		expect(changes[1].state).toContain('"count":2');
	});

	it("biState shared state", () => {
		session = new ProjectionSession(`
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

		session.feed({
			eventType: "Added",
			streamId: "s-1",
			data: '{"amount":10}',
		});
		session.feed({
			eventType: "Added",
			streamId: "s-1",
			data: '{"amount":20}',
		});

		expect(session.getStateJson<{ count: number }>()?.count).toBe(2);
		expect(session.getSharedStateJson<{ total: number }>()?.total).toBe(30);
	});

	it("getResult with transformBy", () => {
		session = new ProjectionSession(`
			fromAll().when({
				$init: function() { return { count: 0 }; },
				Ping: function(s, e) { s.count++; return s; }
			}).transformBy(function(s) {
				return { total: s.count * 2 };
			}).outputState()
		`);

		session.feed({ eventType: "Ping", streamId: "s-1", data: "{}" });

		expect(session.getResultJson<{ total: number }>()?.total).toBe(2);
	});

	it("getPartitionKey", () => {
		session = new ProjectionSession(`
			fromAll().partitionBy(function(e) {
				return e.data.region;
			}).when({
				$init: function() { return {}; },
				Event: function(s, e) { return s; }
			})
		`);

		const key = session.getPartitionKey({
			eventType: "Event",
			streamId: "s-1",
			data: '{"region":"eu"}',
		});
		expect(key).toBe("eu");
	});
});
