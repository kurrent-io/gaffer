using Gaffer.Runtime.Events;

namespace Gaffer.Runtime.Tests;

public class ProjectionSessionTests {
	[Fact]
	public void Simple_count() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                ItemAdded: function(s, e) { s.count++; return s; }
            })
        """);

		session.Feed(new ProjectionEvent { EventType = "ItemAdded", StreamId = "cart-1", Data = "{}" });
		session.Feed(new ProjectionEvent { EventType = "ItemAdded", StreamId = "cart-1", Data = "{}" });
		session.Feed(new ProjectionEvent { EventType = "ItemAdded", StreamId = "cart-1", Data = "{}" });

		var state = session.GetState();
		Assert.NotNull(state);
		Assert.Contains("\"count\":3", state);
	}

	[Fact]
	public void Event_data_accessible() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { total: 0 }; },
                Deposited: function(s, e) { s.total += e.data.amount; return s; }
            })
        """);

		session.Feed(new ProjectionEvent { EventType = "Deposited", StreamId = "acc-1", Data = """{"amount":50}""" });
		session.Feed(new ProjectionEvent { EventType = "Deposited", StreamId = "acc-1", Data = """{"amount":30}""" });

		Assert.Contains("\"total\":80", session.GetState()!);
	}

	[Fact]
	public void Unhandled_events_are_ignored() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                ItemAdded: function(s, e) { s.count++; return s; }
            })
        """);

		session.Feed(new ProjectionEvent { EventType = "ItemAdded", StreamId = "cart-1", Data = "{}" });
		session.Feed(new ProjectionEvent { EventType = "SomethingElse", StreamId = "cart-1", Data = "{}" });

		Assert.Contains("\"count\":1", session.GetState()!);
	}

	[Fact]
	public void Emit_fires_callback() {
		var emitted = new List<EmittedEvent>();
		using var session = new ProjectionSession("""
            fromAll().when({
                OrderPlaced: function(s, e) {
                    emit("notifications", "OrderNotification", { orderId: e.data.orderId });
                    return s;
                }
            })
        """);
		session.OnEmit = e => emitted.Add(e);

		session.Feed(new ProjectionEvent {
			EventType = "OrderPlaced",
			StreamId = "order-1",
			Data = """{"orderId":"ABC"}""",
		});

		Assert.Single(emitted);
		Assert.Equal("notifications", emitted[0].StreamId);
		Assert.Equal("OrderNotification", emitted[0].EventType);
		Assert.Contains("ABC", emitted[0].Data);
	}

	[Fact]
	public void Log_fires_callback() {
		var logs = new List<string>();
		using var session = new ProjectionSession("""
            fromAll().when({
                TestEvent: function(s, e) { log("hello"); return s; }
            })
        """);
		session.OnLog = msg => logs.Add(msg);

		session.Feed(new ProjectionEvent { EventType = "TestEvent", StreamId = "s-1", Data = "{}" });

		Assert.Single(logs);
		Assert.Equal("hello", logs[0]);
	}

	[Fact]
	public void StateChanged_fires_callback() {
		var changes = new List<(string partition, string? state)>();
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """);
		session.OnStateChanged = (p, s) => changes.Add((p, s));

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });
		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Equal(2, changes.Count);
		Assert.Contains("\"count\":1", changes[0].state);
		Assert.Contains("\"count\":2", changes[1].state);
	}

	[Fact]
	public void ForeachStream_partitions_by_stream() {
		using var session = new ProjectionSession("""
            fromCategory("cart").foreachStream().when({
                $init: function() { return { items: 0 }; },
                ItemAdded: function(s, e) { s.items++; return s; }
            })
        """);

		session.Feed(new ProjectionEvent { EventType = "ItemAdded", StreamId = "cart-1", Data = "{}" });
		session.Feed(new ProjectionEvent { EventType = "ItemAdded", StreamId = "cart-1", Data = "{}" });
		session.Feed(new ProjectionEvent { EventType = "ItemAdded", StreamId = "cart-2", Data = "{}" });

		Assert.Contains("\"items\":2", session.GetState("cart-1")!);
		Assert.Contains("\"items\":1", session.GetState("cart-2")!);
	}

	[Fact]
	public void PartitionBy_custom_key() {
		using var session = new ProjectionSession("""
            fromAll().partitionBy(function(e) { return e.data.userId; }).when({
                $init: function() { return { orders: 0 }; },
                OrderPlaced: function(s, e) { s.orders++; return s; }
            })
        """);

		session.Feed(new ProjectionEvent { EventType = "OrderPlaced", StreamId = "order-1", Data = """{"userId":"u1"}""" });
		session.Feed(new ProjectionEvent { EventType = "OrderPlaced", StreamId = "order-2", Data = """{"userId":"u2"}""" });
		session.Feed(new ProjectionEvent { EventType = "OrderPlaced", StreamId = "order-3", Data = """{"userId":"u1"}""" });

		Assert.Contains("\"orders\":2", session.GetState("u1")!);
		Assert.Contains("\"orders\":1", session.GetState("u2")!);
	}

	[Fact]
	public void SlowHandler_fires_callback() {
		var slowHandlers = new List<(string handler, int ms)>();
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                SlowEvent: function(s, e) {
                    var start = Date.now();
                    while (Date.now() - start < 50) {}
                    return s;
                }
            })
        """, new ProjectionSessionOptions { HandlerTimeoutMs = 10 });
		session.OnSlowHandler = (h, ms) => slowHandlers.Add((h, ms));

		session.Feed(new ProjectionEvent { EventType = "SlowEvent", StreamId = "s-1", Data = "{}" });

		Assert.Single(slowHandlers);
		Assert.Equal("SlowEvent", slowHandlers[0].handler);
		Assert.True(slowHandlers[0].ms >= 10);
	}

	[Fact]
	public void TransformBy_affects_result() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            }).transformBy(function(s) {
                return { total: s.count };
            }).outputState()
        """);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		var result = session.GetResult();
		Assert.NotNull(result);
		Assert.Contains("\"total\":1", result);
	}

	[Fact]
	public void SetState_restores_state() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """);

		session.SetState(null, """{"count":10}""");
		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Contains("\"count\":11", session.GetState()!);
	}

	[Fact]
	public void Source_definition_exposed() {
		using var session = new ProjectionSession("""
            fromCategory("orders").foreachStream().when({
                $init: function() { return {}; },
                OrderPlaced: function(s, e) { return s; }
            })
        """);

		Assert.True(session.Sources.ByStreams);
		Assert.NotNull(session.Sources.Categories);
		Assert.Contains("orders", session.Sources.Categories);
	}
}
