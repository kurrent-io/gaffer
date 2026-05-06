import { describe, it, expect, afterEach } from "vitest";
import {
	ProjectionSession,
	InvalidProjectionError,
	ProjectionHandlerError,
	DiagnosticSeverity,
} from "../src/index.js";
import type { EmittedEvent } from "../src/index.js";

describe("ProjectionSession", () => {
	let session: ProjectionSession | null = null;

	afterEach(() => {
		session?.dispose();
		session = null;
	});

	it("creates and destroys a session", () => {
		session = new ProjectionSession(
			`
			fromAll().when({
				$init() { return {}; },
				Ping(s, e) { return s; }
			})
		`,
			{ engineVersion: 2 },
		);
	});

	it("throws on invalid JS", () => {
		expect(
			() =>
				new ProjectionSession("this is not valid {{{{", { engineVersion: 2 }),
		).toThrow(InvalidProjectionError);
	});

	it("feeds events and gets state", () => {
		session = new ProjectionSession(
			`
			fromAll().when({
				$init() { return { count: 0 }; },
				ItemAdded(s, e) { s.count++; return s; }
			})
		`,
			{ engineVersion: 2 },
		);

		session.feed({
			eventType: "ItemAdded",
			streamId: "cart-1",
			sequenceNumber: 0,
			data: "{}",
			isJson: true,
			eventId: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
		});
		session.feed({
			eventType: "ItemAdded",
			streamId: "cart-1",
			sequenceNumber: 0,
			data: "{}",
			isJson: true,
			eventId: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
		});
		session.feed({
			eventType: "ItemAdded",
			streamId: "cart-1",
			sequenceNumber: 0,
			data: "{}",
			isJson: true,
			eventId: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
		});

		expect(session.getStateJson<{ count: number }>()?.count).toBe(3);
	});

	it("accesses event data", () => {
		session = new ProjectionSession(
			`
			fromAll().when({
				$init() { return { total: 0 }; },
				Deposited(s, e) { s.total += e.data.amount; return s; }
			})
		`,
			{ engineVersion: 2 },
		);

		session.feed({
			eventType: "Deposited",
			streamId: "acc-1",
			sequenceNumber: 0,
			data: '{"amount":50}',
			isJson: true,
			eventId: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
		});
		session.feed({
			eventType: "Deposited",
			streamId: "acc-1",
			sequenceNumber: 0,
			data: '{"amount":30}',
			isJson: true,
			eventId: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
		});

		expect(session.getStateJson<{ total: number }>()?.total).toBe(80);
	});

	it("partitions by stream", () => {
		session = new ProjectionSession(
			`
			fromCategory("cart").foreachStream().when({
				$init() { return { items: 0 }; },
				ItemAdded(s, e) { s.items++; return s; }
			})
		`,
			{ engineVersion: 2 },
		);

		session.feed({
			eventType: "ItemAdded",
			streamId: "cart-1",
			sequenceNumber: 0,
			data: "{}",
			isJson: true,
			eventId: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
		});
		session.feed({
			eventType: "ItemAdded",
			streamId: "cart-1",
			sequenceNumber: 0,
			data: "{}",
			isJson: true,
			eventId: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
		});
		session.feed({
			eventType: "ItemAdded",
			streamId: "cart-2",
			sequenceNumber: 0,
			data: "{}",
			isJson: true,
			eventId: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
		});

		expect(session.getStateJson<{ items: number }>("cart-1")?.items).toBe(2);
		expect(session.getStateJson<{ items: number }>("cart-2")?.items).toBe(1);
	});

	it("gets source definition", () => {
		session = new ProjectionSession(
			`
			fromAll().foreachStream().when({
				$init() { return {}; },
				Ping(s, e) { return s; }
			})
		`,
			{ engineVersion: 2 },
		);

		const sources = session.getSources();
		expect(sources.allStreams).toBe(true);
		expect(sources.byStreams).toBe(true);
		expect(sources.diagnostics).toBeNull();
	});

	it("reports linkStreamTo as a deprecation diagnostic", () => {
		session = new ProjectionSession(
			`
			fromAll().when({
				$any: function (s, e) {
					linkStreamTo("archive-" + e.streamId, e.streamId);
					return s;
				}
			})
		`,
			{ engineVersion: 2 },
		);

		const sources = session.getSources();
		expect(sources.diagnostics).toHaveLength(1);
		const d = sources.diagnostics?.[0];
		expect(d?.code).toBe("deprecated.linkStreamTo");
		expect(d?.severity).toBe(DiagnosticSeverity.Warning);
		expect(d?.message).toContain("linkStreamTo");
		expect(d?.range).not.toBeNull();
		const span = (d?.range?.end.column ?? 0) - (d?.range?.start.column ?? 0);
		expect(span).toBe("linkStreamTo".length);
	});

	it("sets and restores state", () => {
		session = new ProjectionSession(
			`
			fromAll().when({
				$init() { return { count: 0 }; },
				Ping(s, e) { s.count++; return s; }
			})
		`,
			{ engineVersion: 2 },
		);

		session.setState(null, '{"count":10}');
		session.feed({
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			data: "{}",
			isJson: true,
			eventId: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
		});

		expect(session.getStateJson<{ count: number }>()?.count).toBe(11);
	});

	it("throws on handler error", () => {
		session = new ProjectionSession(
			`
			fromAll().when({
				$init() { return {}; },
				Bad(s, e) { throw "boom"; }
			})
		`,
			{ engineVersion: 2 },
		);

		expect(() =>
			session?.feed({
				eventType: "Bad",
				streamId: "s-1",
				sequenceNumber: 0,
				data: "{}",
				isJson: true,
				eventId: "00000000-0000-0000-0000-000000000000",
				created: "2026-01-01T00:00:00Z",
			}),
		).toThrow(ProjectionHandlerError);
	});

	it("returns null for unknown partition", () => {
		session = new ProjectionSession(
			`
			fromAll().foreachStream().when({
				$init() { return {}; },
				Ping(s, e) { return s; }
			})
		`,
			{ engineVersion: 2 },
		);

		expect(session.getState("nonexistent")).toBeNull();
	});

	it("throws after dispose", () => {
		const s = new ProjectionSession(
			`
			fromAll().when({
				$init() { return {}; },
				Ping(s, e) { return s; }
			})
		`,
			{ engineVersion: 2 },
		);
		s.dispose();

		expect(() =>
			s.feed({
				eventType: "Ping",
				streamId: "s-1",
				sequenceNumber: 0,
				data: "{}",
				isJson: true,
				eventId: "00000000-0000-0000-0000-000000000000",
				created: "2026-01-01T00:00:00Z",
			}),
		).toThrow(/disposed/);
	});

	it("double dispose is safe", () => {
		const s = new ProjectionSession(
			`
			fromAll().when({
				$init() { return {}; },
				Ping(s, e) { return s; }
			})
		`,
			{ engineVersion: 2 },
		);
		s.dispose();
		s.dispose(); // should not throw
	});

	it("onEmit receives emitted events", () => {
		session = new ProjectionSession(
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

		const emitted: EmittedEvent[] = [];
		session.onEmit((e) => emitted.push(e));

		session.feed({
			eventType: "OrderPlaced",
			streamId: "order-1",
			sequenceNumber: 0,
			data: '{"orderId":"ABC"}',
			isJson: true,
			eventId: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
		});

		expect(emitted).toHaveLength(1);
		const e0 = emitted[0];
		expect(e0).toBeDefined();
		expect(e0?.streamId).toBe("notifications");
		expect(e0?.eventType).toBe("OrderNotification");
		expect(e0?.data).toContain("ABC");
	});

	it("onLog captures console.log", () => {
		session = new ProjectionSession(
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

		const logs: string[] = [];
		session.onLog((msg) => logs.push(msg));

		session.feed({
			eventType: "TestEvent",
			streamId: "s-1",
			sequenceNumber: 0,
			data: "{}",
			isJson: true,
			eventId: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
		});

		expect(logs).toHaveLength(1);
		expect(logs[0]).toBe("hello from projection");
	});

	it("onStateChanged fires on state update", () => {
		session = new ProjectionSession(
			`
			fromAll().when({
				$init() { return { count: 0 }; },
				Ping(s, e) { s.count++; return s; }
			})
		`,
			{ engineVersion: 2 },
		);

		const changes: Array<{ partition: string; state: string | null }> = [];
		session.onStateChanged((partition, state) =>
			changes.push({ partition, state }),
		);

		session.feed({
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			data: "{}",
			isJson: true,
			eventId: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
		});
		session.feed({
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			data: "{}",
			isJson: true,
			eventId: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
		});

		expect(changes).toHaveLength(2);
		expect(changes[0]?.state).toContain('"count":1');
		expect(changes[1]?.state).toContain('"count":2');
	});

	it("biState shared state", () => {
		session = new ProjectionSession(
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

		session.feed({
			eventType: "Added",
			streamId: "s-1",
			sequenceNumber: 0,
			data: '{"amount":10}',
			isJson: true,
			eventId: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
		});
		session.feed({
			eventType: "Added",
			streamId: "s-1",
			sequenceNumber: 0,
			data: '{"amount":20}',
			isJson: true,
			eventId: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
		});

		expect(session.getStateJson<{ count: number }>()?.count).toBe(2);
		expect(session.getSharedStateJson<{ total: number }>()?.total).toBe(30);
	});

	it("getResult with transformBy", () => {
		session = new ProjectionSession(
			`
			fromAll().when({
				$init() { return { count: 0 }; },
				Ping(s, e) { s.count++; return s; }
			}).transformBy(function(s) {
				return { total: s.count * 2 };
			}).outputState()
		`,
			{ engineVersion: 2 },
		);

		session.feed({
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			data: "{}",
			isJson: true,
			eventId: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
		});

		expect(session.getResultJson<{ total: number }>()?.total).toBe(2);
	});

	it("feed returns processed result with state", () => {
		session = new ProjectionSession(
			`
			fromAll().when({
				$init() { return { count: 0 }; },
				Ping(s, e) { s.count++; return s; }
			})
		`,
			{ engineVersion: 2 },
		);

		const result = session.feed({
			eventType: "Ping",
			streamId: "s-1",
			sequenceNumber: 0,
			data: "{}",
			isJson: true,
			eventId: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
		});

		expect(result.status).toBe("processed");
		expect(result.state).toBeDefined();
		expect((result.state as { count: number }).count).toBe(1);
	});

	it("feed returns skipped for unhandled event", () => {
		session = new ProjectionSession(
			`
			fromAll().when({
				$init() { return {}; },
				Ping(s, e) { return s; }
			})
		`,
			{ engineVersion: 2 },
		);

		const result = session.feed({
			eventType: "UnhandledEvent",
			streamId: "s-1",
			sequenceNumber: 0,
			data: "{}",
			isJson: true,
			eventId: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
		});

		expect(result.status).toBe("skipped");
		expect(result.reason).toBe("unhandled");
	});

	it("feed returns emitted events in result", () => {
		session = new ProjectionSession(
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

		const result = session.feed({
			eventType: "OrderPlaced",
			streamId: "order-1",
			sequenceNumber: 0,
			data: '{"orderId":"ABC"}',
			isJson: true,
			eventId: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
		});

		expect(result.status).toBe("processed");
		expect(result.emitted).toBeDefined();
		expect(result.emitted).toHaveLength(1);
		expect(result.emitted?.[0]?.streamId).toBe("notifications");
		expect(result.emitted?.[0]?.eventType).toBe("OrderNotification");
	});

	it("feed returns logs in result", () => {
		session = new ProjectionSession(
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

		const result = session.feed({
			eventType: "TestEvent",
			streamId: "s-1",
			sequenceNumber: 0,
			data: "{}",
			isJson: true,
			eventId: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
		});

		expect(result.status).toBe("processed");
		expect(result.logs).toBeDefined();
		expect(result.logs).toHaveLength(1);
		expect(result.logs?.[0]).toBe("hello from projection");
	});

	it("feed returns partition for foreachStream", () => {
		session = new ProjectionSession(
			`
			fromAll().foreachStream().when({
				$init() { return { count: 0 }; },
				Ping(s, e) { s.count++; return s; }
			})
		`,
			{ engineVersion: 2 },
		);

		const result = session.feed({
			eventType: "Ping",
			streamId: "order-42",
			sequenceNumber: 0,
			data: "{}",
			isJson: true,
			eventId: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
		});

		expect(result.status).toBe("processed");
		expect(result.partition).toBe("order-42");
	});

	it("getPartitionKey", () => {
		session = new ProjectionSession(
			`
			fromAll().partitionBy(function(e) {
				return e.data.region;
			}).when({
				$init() { return {}; },
				Event(s, e) { return s; }
			})
		`,
			{ engineVersion: 2 },
		);

		const key = session.getPartitionKey({
			eventType: "Event",
			streamId: "s-1",
			sequenceNumber: 0,
			data: '{"region":"eu"}',
			isJson: true,
			eventId: "00000000-0000-0000-0000-000000000000",
			created: "2026-01-01T00:00:00Z",
		});
		expect(key).toBe("eu");
	});
});
