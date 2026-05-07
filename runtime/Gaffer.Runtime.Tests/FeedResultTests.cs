using Gaffer.Runtime.Events;

namespace Gaffer.Runtime.Tests;

public class FeedResultTests {
	// -- Status --

	[Fact]
	public void Processed_event_returns_processed_status() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Equal(FeedStatus.Processed, result.Status);
	}

	[Fact]
	public void Skipped_event_returns_skipped_status() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent { EventType = "Unhandled", StreamId = "s-1", Data = "{}" });

		Assert.Equal(FeedStatus.Skipped, result.Status);
		Assert.Equal("unhandled", result.SkipReason);
	}

	[Fact]
	public void V1_non_json_returns_skipped() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                $any: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V1 });

		var result = session.Feed(new ProjectionEvent { EventType = "Bin", StreamId = "s-1", Data = "x", IsJson = false });

		Assert.Equal(FeedStatus.Skipped, result.Status);
		Assert.Equal("non-json", result.SkipReason);
		Assert.Null(result.Partition);
		Assert.Null(result.State);
		Assert.Empty(result.Emitted);
		Assert.Empty(result.Logs);
	}

	[Fact]
	public void Link_event_returns_skipped() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                $any: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent { EventType = "$>", StreamId = "$ce-order", Data = "0@order-1", IsJson = false });

		Assert.Equal(FeedStatus.Skipped, result.Status);
		Assert.Equal("link", result.SkipReason);
	}

	// -- State and partition --

	[Fact]
	public void Processed_result_contains_state() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Contains("\"count\":1", result.State!);
	}

	[Fact]
	public void Unpartitioned_result_has_null_partition() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                Ping: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Null(result.Partition);
	}

	[Fact]
	public void ForeachStream_result_has_stream_partition() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "order-1", Data = "{}" });

		Assert.Equal("order-1", result.Partition);
		Assert.Contains("\"count\":1", result.State!);
	}

	// -- Result / transforms --

	[Fact]
	public void Result_equals_state_when_no_transforms() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Equal(result.State, result.Result);
	}

	[Fact]
	public void Result_reflects_transformBy() {
		// V1 only - V2 doesn't iterate transforms; see V2ConformanceTests.
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            }).transformBy(function(s) {
                return { doubled: s.count * 2 };
            }).outputState()
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V1 });

		var result = session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Contains("\"count\":1", result.State!);
		Assert.Contains("\"doubled\":2", result.Result!);
	}

	[Fact]
	public void Result_null_when_filterBy_excludes() {
		// V1 only - V2 doesn't iterate transforms; see V2ConformanceTests.
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            }).filterBy(function(s) {
                return s.count >= 3;
            }).outputState()
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V1 });

		var result = session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Contains("\"count\":1", result.State!);
		Assert.Null(result.Result);
	}

	// -- Shared state --

	[Fact]
	public void BiState_result_includes_shared_state() {
		using var session = new ProjectionSession("""
            options({ biState: true });
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $initShared: function() { return { total: 0 }; },
                Add: function(s, e) {
                    s[0].count++;
                    s[1].total += e.data.amount;
                    return s;
                }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent { EventType = "Add", StreamId = "s-1", Data = """{"amount":10}""" });

		Assert.Contains("\"count\":1", result.State!);
		Assert.Contains("\"total\":10", result.SharedState!);
	}

	[Fact]
	public void Non_bistate_result_has_null_shared_state() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                Ping: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Null(result.SharedState);
	}

	// -- Emitted events --

	[Fact]
	public void Emitted_events_included_in_result() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                Order: function(s, e) {
                    emit("target", "Forwarded", { id: 1 });
                    return s;
                }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent { EventType = "Order", StreamId = "s-1", Data = "{}" });

		Assert.Single(result.Emitted);
		Assert.Equal("target", result.Emitted[0].StreamId);
		Assert.Equal("Forwarded", result.Emitted[0].EventType);
	}

	[Fact]
	public void Created_emits_included_in_result() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return {}; },
                $created: function(s, e) {
                    emit("audit", "StreamCreated", { stream: e.streamId });
                },
                Ping: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Contains(result.Emitted, e => e.StreamId == "audit" && e.EventType == "StreamCreated");
	}

	[Fact]
	public void No_emits_returns_empty_array() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                Ping: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Empty(result.Emitted);
	}

	// -- Logs --

	[Fact]
	public void Logs_included_in_result() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                Ping: function(s, e) {
                    log("hello");
                    log("world");
                    return s;
                }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Equal(2, result.Logs.Length);
		Assert.Equal("hello", result.Logs[0]);
		Assert.Equal("world", result.Logs[1]);
	}

	[Fact]
	public void No_logs_returns_empty_array() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                Ping: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Empty(result.Logs);
	}

	// -- Delete events --

	[Fact]
	public void Stream_deleted_returns_processed_result() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { deleted: false }; },
                Ping: function(s, e) { return s; },
                $deleted: function(s, e) { s.deleted = true; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "order-1", Data = "{}" });
		var result = session.Feed(new ProjectionEvent { EventType = "$streamDeleted", StreamId = "order-1", Data = "{}" });

		Assert.Equal(FeedStatus.Processed, result.Status);
		Assert.Equal("order-1", result.Partition);
		Assert.Contains("\"deleted\":true", result.State!);
	}

	[Fact]
	public void Delete_without_handler_returns_skipped() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return {}; },
                Ping: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent { EventType = "$streamDeleted", StreamId = "order-1", Data = "{}" });

		Assert.Equal(FeedStatus.Skipped, result.Status);
		Assert.Equal("no-delete-handler", result.SkipReason);
	}

	// -- Callbacks still fire --

	[Fact]
	public void Callbacks_fire_alongside_result() {
		var emitCb = new List<EmittedEvent>();
		var logCb = new List<string>();
		var stateCb = new List<string?>();

		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) {
                    s.count++;
                    emit("target", "Fwd", {});
                    log("processed");
                    return s;
                }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });
		session.OnEmit = e => emitCb.Add(e);
		session.OnLog = m => logCb.Add(m);
		session.OnStateChanged = (_, s) => stateCb.Add(s);

		var result = session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Single(emitCb);
		Assert.Single(logCb);
		Assert.Single(stateCb);

		Assert.Equal(result.Emitted[0].StreamId, emitCb[0].StreamId);
		Assert.Equal(result.Logs[0], logCb[0]);
		Assert.Contains("\"count\":1", stateCb[0]!);
	}

	// -- Isolation between feeds --

	[Fact]
	public void Emits_not_leaked_between_feeds() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                Emit: function(s, e) { emit("target", "Fwd", {}); return s; },
                NoEmit: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var r1 = session.Feed(new ProjectionEvent { EventType = "Emit", StreamId = "s-1", Data = "{}" });
		var r2 = session.Feed(new ProjectionEvent { EventType = "NoEmit", StreamId = "s-1", Data = "{}" });

		Assert.Single(r1.Emitted);
		Assert.Empty(r2.Emitted);
	}

	[Fact]
	public void Logs_not_leaked_between_feeds() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                WithLog: function(s, e) { log("hello"); return s; },
                NoLog: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var r1 = session.Feed(new ProjectionEvent { EventType = "WithLog", StreamId = "s-1", Data = "{}" });
		var r2 = session.Feed(new ProjectionEvent { EventType = "NoLog", StreamId = "s-1", Data = "{}" });

		Assert.Single(r1.Logs);
		Assert.Empty(r2.Logs);
	}

	// -- Consistency with getters --

	[Fact]
	public void Result_state_matches_GetState() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Equal(result.State, session.GetState());
	}

	[Fact]
	public void Result_result_matches_GetResult() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            }).transformBy(function(s) {
                return { doubled: s.count * 2 };
            }).outputState()
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Equal(result.Result, session.GetResult());
	}

	[Fact]
	public void Result_shared_state_matches_GetSharedState() {
		using var session = new ProjectionSession("""
            options({ biState: true });
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $initShared: function() { return { total: 0 }; },
                Add: function(s, e) { s[0].count++; s[1].total += e.data.amount; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent { EventType = "Add", StreamId = "s-1", Data = """{"amount":5}""" });

		Assert.Equal(result.SharedState, session.GetSharedState());
	}

	// -- Null state --

	[Fact]
	public void Handler_returns_null_state_and_result_are_null() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Clear: function(s, e) { return null; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent { EventType = "Clear", StreamId = "s-1", Data = "{}" });

		Assert.Equal(FeedStatus.Processed, result.Status);
		Assert.Null(result.State);
		Assert.Null(result.Result);
	}

	// -- Additional skip paths --

	[Fact]
	public void PartitionBy_null_returns_skipped() {
		using var session = new ProjectionSession("""
            fromAll().partitionBy(function(e) {
                return null;
            }).when({
                $init: function() { return {}; },
                $any: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Equal(FeedStatus.Skipped, result.Status);
		Assert.Equal("no-partition", result.SkipReason);
		Assert.Null(result.Partition);
		Assert.Null(result.State);
		Assert.Empty(result.Emitted);
		Assert.Empty(result.Logs);
	}

	[Fact]
	public void Resolved_link_event_returns_skipped() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                $any: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent {
			EventType = "OrderPlaced",
			StreamId = "order-1",
			Data = "{}",
			LinkMetadata = """{"$o":"order-1"}""",
		});

		Assert.Equal(FeedStatus.Skipped, result.Status);
		Assert.Equal("link", result.SkipReason);
	}

	// -- Additional emit/link coverage --

	[Fact]
	public void Multiple_emits_in_result() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                Order: function(s, e) {
                    emit("stream-a", "First", {});
                    emit("stream-b", "Second", {});
                    return s;
                }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent { EventType = "Order", StreamId = "s-1", Data = "{}" });

		Assert.Equal(2, result.Emitted.Length);
		Assert.Equal("stream-a", result.Emitted[0].StreamId);
		Assert.Equal("stream-b", result.Emitted[1].StreamId);
	}

	[Fact]
	public void LinkTo_emitted_has_isLink_true() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                Order: function(s, e) { linkTo("target", e); return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent { EventType = "Order", StreamId = "s-1", Data = "{}", SequenceNumber = 5 });

		Assert.Single(result.Emitted);
		Assert.True(result.Emitted[0].IsLink);
		Assert.Equal("$>", result.Emitted[0].EventType);
	}

	// -- State accumulation --

	[Fact]
	public void Second_feed_result_reflects_accumulated_state() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });
		var result = session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Contains("\"count\":2", result.State!);
	}

	// -- Custom partitionBy key --

	[Fact]
	public void PartitionBy_custom_key_in_result() {
		using var session = new ProjectionSession("""
            fromAll().partitionBy(function(e) {
                return e.body.region;
            }).when({
                $init: function() { return { count: 0 }; },
                Order: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var result = session.Feed(new ProjectionEvent { EventType = "Order", StreamId = "s-1", Data = """{"region":"eu"}""" });

		Assert.Equal("eu", result.Partition);
		Assert.Contains("\"count\":1", result.State!);
	}

	// -- Soft delete --

	[Fact]
	public void Soft_delete_result_has_original_stream_partition() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { deleted: false }; },
                Ping: function(s, e) { return s; },
                $deleted: function(s, e) { s.deleted = true; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "order-1", Data = "{}" });
		var result = session.Feed(new ProjectionEvent {
			EventType = "$metadata",
			StreamId = "$$order-1",
			Data = """{"$tb":9223372036854775807}""",
		});

		Assert.Equal(FeedStatus.Processed, result.Status);
		Assert.Equal("order-1", result.Partition);
		Assert.Contains("\"deleted\":true", result.State!);
	}
}
