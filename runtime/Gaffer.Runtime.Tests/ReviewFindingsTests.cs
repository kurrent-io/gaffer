using Gaffer.Runtime.Events;

namespace Gaffer.Runtime.Tests;

public class ReviewFindingsTests {
	// -- linkTo / linkStreamTo at session level --

	[Fact]
	public void LinkTo_at_session_level() {
		var emitted = new List<EmittedEvent>();
		using var session = new ProjectionSession("""
            fromAll().when({
                OrderPlaced: function(s, e) {
                    linkTo("orders-by-customer", e);
                    return s;
                }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });
		session.OnEmit = e => emitted.Add(e);

		session.Feed(new ProjectionEvent {
			EventType = "OrderPlaced",
			StreamId = "order-1",
			Data = """{"customerId":"C1"}""",
			SequenceNumber = 42,
		});

		Assert.Single(emitted);
		Assert.Equal("orders-by-customer", emitted[0].StreamId);
		Assert.Equal("$>", emitted[0].EventType);
		Assert.True(emitted[0].IsLink);
		Assert.False(emitted[0].IsJson);
		Assert.Equal("42@order-1", emitted[0].Data);
	}

	[Fact]
	public void LinkStreamTo_at_session_level() {
		var emitted = new List<EmittedEvent>();
		using var session = new ProjectionSession("""
            fromAll().when({
                StreamMoved: function(s, e) {
                    linkStreamTo("archive-" + e.streamId, e.streamId);
                    return s;
                }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });
		session.OnEmit = e => emitted.Add(e);

		session.Feed(new ProjectionEvent {
			EventType = "StreamMoved",
			StreamId = "old-stream",
			Data = "{}",
		});

		Assert.Single(emitted);
		Assert.Equal("archive-old-stream", emitted[0].StreamId);
		Assert.Equal("$@", emitted[0].EventType);
		Assert.False(emitted[0].IsJson);
		Assert.Equal("old-stream", emitted[0].Data);
	}

	// -- emit with metadata --

	[Fact]
	public void Emit_with_metadata() {
		var emitted = new List<EmittedEvent>();
		using var session = new ProjectionSession("""
            fromAll().when({
                TestEvent: function(s, e) {
                    emit("output", "Processed", { id: 1 }, { source: "test", correlationId: "abc" });
                    return s;
                }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });
		session.OnEmit = e => emitted.Add(e);

		session.Feed(new ProjectionEvent { EventType = "TestEvent", StreamId = "s-1", Data = "{}" });

		Assert.Single(emitted);
		Assert.NotNull(emitted[0].Metadata);
		Assert.True(emitted[0].Metadata!.ContainsKey("source"));
		Assert.True(emitted[0].Metadata!.ContainsKey("correlationId"));
	}

	// -- partitionBy returning null/undefined/number --

	[Fact]
	public void PartitionBy_returning_null_skips_event() {
		using var session = new ProjectionSession("""
            fromAll().partitionBy(function(e) {
                return null;
            }).when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Null(session.GetState(""));
	}

	[Fact]
	public void PartitionBy_returning_undefined_skips_event() {
		using var session = new ProjectionSession("""
            fromAll().partitionBy(function(e) {
                return undefined;
            }).when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Null(session.GetState(""));
	}

	[Fact]
	public void PartitionBy_returning_number() {
		using var session = new ProjectionSession("""
            fromAll().partitionBy(function(e) {
                return e.data.region;
            }).when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = """{"region":42}""" });

		Assert.Contains("\"count\":1", session.GetState("42")!);
	}

	// -- ResolveCategory edge cases --

	[Fact]
	public void Category_from_stream_with_no_dash() {
		using var session = new ProjectionSession("""
            fromCategory("mystream").foreachStream().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "mystream", Data = "{}" });

		Assert.Contains("\"count\":1", session.GetState("mystream")!);
	}

	[Fact]
	public void Category_from_stream_with_multiple_dashes() {
		using var session = new ProjectionSession("""
            fromCategory("order").foreachStream().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "order-item-123", Data = "{}" });

		// Category is "order" (first segment before dash)
		Assert.Contains("\"count\":1", session.GetState("order-item-123")!);
	}

	// -- BiState shared state across partitions --

	[Fact]
	public void BiState_shared_state_visible_across_partitions() {
		using var session = new ProjectionSession("""
            options({ biState: true });
            fromAll().foreachStream().when({
                $init: function() { return { count: 0 }; },
                $initShared: function() { return { total: 0 }; },
                Add: function(s, e) {
                    s[0].count++;
                    s[1].total += e.data.amount;
                    return s;
                }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Add", StreamId = "a", Data = """{"amount":10}""" });
		session.Feed(new ProjectionEvent { EventType = "Add", StreamId = "b", Data = """{"amount":20}""" });
		session.Feed(new ProjectionEvent { EventType = "Add", StreamId = "a", Data = """{"amount":30}""" });

		Assert.Contains("\"count\":2", session.GetState("a")!);
		Assert.Contains("\"count\":1", session.GetState("b")!);
		// Shared state accumulates across all partitions
		Assert.Contains("\"total\":60", session.GetSharedState()!);
	}

	// -- SetState then GetResult with transforms --

	[Fact]
	public void SetState_then_GetResult_with_transform() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            }).transformBy(function(s) {
                return { total: s.count * 2 };
            }).outputState()
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.SetState(null, """{"count":5}""");
		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		var result = session.GetResult();
		Assert.NotNull(result);
		Assert.Contains("\"total\":12", result);
	}

	// -- OnStateChanged not called for null state --

	[Fact]
	public void OnStateChanged_not_called_when_handler_returns_null() {
		var changes = new List<string?>();
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; },
                Reset: function(s, e) { return null; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });
		session.OnStateChanged = (_, s) => changes.Add(s);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });
		session.Feed(new ProjectionEvent { EventType = "Reset", StreamId = "s-1", Data = "{}" });

		// Only the Ping fires OnStateChanged, not the null-returning Reset
		Assert.Single(changes);
	}

	// -- on_event/on_any don't set DefinesFold --

	[Fact]
	public void On_event_does_not_set_DefinesFold() {
		using var session = new ProjectionSession("""
            fromAll();
            on_event("Ping", function(s, e) { return s; });
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.False(session.Sources.DefinesFold);
	}

	[Fact]
	public void When_sets_DefinesFold() {
		using var session = new ProjectionSession("""
            fromAll().when({
                Ping: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.True(session.Sources.DefinesFold);
	}

	// -- Content type validation --

	[Fact]
	public void Non_json_empty_data_processed() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Bin", StreamId = "s-1", Data = "", IsJson = false });

		Assert.Contains("\"count\":1", session.GetState()!);
	}

	// -- Malformed soft-delete metadata --

	[Fact]
	public void Malformed_metadata_throws() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { a: 0 }; },
                type1: function(s, e) { s.a++; return s; },
                $deleted: function(s, e) { s.deleted = 1; return s; }
            }).outputState()
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.ThrowsAny<Exception>(() =>
			session.Feed(new ProjectionEvent {
				EventType = "$metadata",
				StreamId = "$$stream-1",
				Data = "not json at all",
			}));
	}
}
