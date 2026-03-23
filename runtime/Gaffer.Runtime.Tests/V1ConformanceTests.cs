using Gaffer.Runtime.Errors;
using Gaffer.Runtime.Events;

namespace Gaffer.Runtime.Tests;

public class V1ConformanceTests {
	private static readonly ProjectionSessionOptions V1 = new() { Version = ProjectionVersion.V1 };

	// --- Event filtering ---

	[Fact]
	public void V1_drops_non_json_events() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Bin", StreamId = "s-1", Data = "binary", IsJson = false });

		Assert.Null(session.GetState());
	}

	[Fact]
	public void V1_processes_json_events() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}", IsJson = true });

		Assert.Contains("\"count\":1", session.GetState()!);
	}

	[Fact]
	public void V1_skips_json_event_with_null_data() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = null, IsJson = true });

		Assert.Contains("\"count\":0", session.GetState()!);
	}

	[Fact]
	public void V1_skips_json_event_with_empty_data() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "", IsJson = true });

		Assert.Contains("\"count\":0", session.GetState()!);
	}

	[Fact]
	public void V1_skips_json_event_with_whitespace_data() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "  ", IsJson = true });

		Assert.Contains("\"count\":0", session.GetState()!);
	}

	// --- System events ---

	[Fact]
	public void V1_hard_delete_processed_even_when_non_json() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { deleted: false }; },
                Ping: function(s, e) { return s; },
                $deleted: function(s, e) { s.deleted = true; return s; }
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "order-1", Data = "{}", IsJson = true });
		session.Feed(new ProjectionEvent { EventType = "$streamDeleted", StreamId = "order-1", IsJson = false });

		Assert.Contains("\"deleted\":true", session.GetState("order-1")!);
	}

	[Fact]
	public void V1_hard_delete_triggers_deleted_handler() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { deleted: false }; },
                Ping: function(s, e) { return s; },
                $deleted: function(s, e) { s.deleted = true; return s; }
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "order-1", Data = "{}", IsJson = true });
		session.Feed(new ProjectionEvent { EventType = "$streamDeleted", StreamId = "order-1", Data = "{}", IsJson = true });

		Assert.Contains("\"deleted\":true", session.GetState("order-1")!);
	}

	[Fact]
	public void V1_soft_delete_triggers_deleted_handler() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { deleted: false }; },
                Ping: function(s, e) { return s; },
                $deleted: function(s, e) { s.deleted = true; return s; }
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "order-1", Data = "{}", IsJson = true });
		session.Feed(new ProjectionEvent {
			EventType = "$metadata",
			StreamId = "$$order-1",
			Data = """{"$tb":9223372036854775807}""",
			IsJson = true,
		});

		Assert.Contains("\"deleted\":true", session.GetState("order-1")!);
	}

	[Fact]
	public void V1_metadata_without_tb_is_normal_event() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, V1);

		session.Feed(new ProjectionEvent {
			EventType = "$metadata",
			StreamId = "$$stream-1",
			Data = """{"$maxAge":100}""",
			IsJson = true,
		});

		Assert.Contains("\"count\":1", session.GetState()!);
	}

	[Fact]
	public void V1_metadata_with_null_data_not_treated_as_delete() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, V1);

		session.Feed(new ProjectionEvent {
			EventType = "$metadata",
			StreamId = "$$stream-1",
			Data = null,
			IsJson = true,
		});

		Assert.Contains("\"count\":0", session.GetState()!);
	}

	[Fact]
	public void V1_malformed_metadata_throws() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return {}; },
                Ping: function(s, e) { return s; },
                $deleted: function(s, e) { s.deleted = true; return s; }
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "order-1", Data = "{}", IsJson = true });

		Assert.Throws<MalformedEventException>(() => session.Feed(new ProjectionEvent {
			EventType = "$metadata",
			StreamId = "$$order-1",
			Data = "not json",
			IsJson = true,
		}));
	}

	// --- Null state ---

	[Fact]
	public void V1_null_state_resets_to_init_on_revisit() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { count: 0 }; },
                Add: function(s, e) { s.count++; return s; },
                Reset: function(s, e) { return null; }
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Add", StreamId = "s-1", Data = "{}", IsJson = true });
		session.Feed(new ProjectionEvent { EventType = "Reset", StreamId = "s-1", Data = "{}", IsJson = true });
		session.Feed(new ProjectionEvent { EventType = "Add", StreamId = "s-1", Data = "{}", IsJson = true });

		Assert.Contains("\"count\":1", session.GetState("s-1")!);
	}

	[Fact]
	public void V1_undefined_return_preserves_mutated_state() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; }
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}", IsJson = true });

		Assert.Contains("\"count\":1", session.GetState()!);
	}

	// --- Partition lifecycle ---

	[Fact]
	public void V1_created_handler_called_on_first_partition_visit() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { created: false }; },
                $created: function(s, e) { s.created = true; },
                Ping: function(s, e) { return s; }
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}", IsJson = true });

		Assert.Contains("\"created\":true", session.GetState("s-1")!);
	}

	[Fact]
	public void V1_created_handler_not_called_on_revisit() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { createdCount: 0 }; },
                $created: function(s, e) { s.createdCount++; },
                Ping: function(s, e) { return s; }
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}", IsJson = true });
		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}", IsJson = true });

		Assert.Contains("\"createdCount\":1", session.GetState("s-1")!);
	}

	[Fact]
	public void V1_partitionBy_null_skips_event() {
		using var session = new ProjectionSession("""
            fromAll().partitionBy(function(e) {
                if (e.eventType === "Skip") return null;
                return e.streamId;
            }).when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Skip", StreamId = "s-1", Data = "{}", IsJson = true });
		session.Feed(new ProjectionEvent { EventType = "Count", StreamId = "s-1", Data = "{}", IsJson = true });

		Assert.Contains("\"count\":1", session.GetState("s-1")!);
	}

	[Fact]
	public void V1_partitionBy_undefined_skips_event() {
		using var session = new ProjectionSession("""
            fromAll().partitionBy(function(e) {
                if (e.eventType === "Skip") return undefined;
                return e.streamId;
            }).when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Skip", StreamId = "s-1", Data = "{}", IsJson = true });
		session.Feed(new ProjectionEvent { EventType = "Count", StreamId = "s-1", Data = "{}", IsJson = true });

		Assert.Contains("\"count\":1", session.GetState("s-1")!);
	}

	[Fact]
	public void V1_partitionBy_number_converted_to_string() {
		using var session = new ProjectionSession("""
            fromAll().partitionBy(function(e) {
                return 42;
            }).when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}", IsJson = true });

		Assert.Contains("\"count\":1", session.GetState("42")!);
	}

	// --- Event type filtering ---

	[Fact]
	public void V1_specific_handlers_filter_unmatched_events() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                A: function(s, e) { s.count++; return s; }
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "B", StreamId = "s-1", Data = "{}", IsJson = true });

		Assert.Null(session.GetState());
	}

	[Fact]
	public void V1_any_handler_catches_unmatched_events() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                A: function(s, e) { return s; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "B", StreamId = "s-1", Data = "{}", IsJson = true });

		Assert.Contains("\"count\":1", session.GetState()!);
	}

	[Fact]
	public void V1_unhandled_event_replaces_state_with_body() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; }
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Unhandled", StreamId = "s-1", Data = """{"x":1}""", IsJson = true });

		Assert.Equal("{\"x\":1}", session.GetState());
	}

	// --- $deleted constraints ---

	[Fact]
	public void V1_deleted_handler_requires_foreach_stream() {
		Assert.Throws<InvalidProjectionException>(() => new ProjectionSession("""
            fromAll().partitionBy(function(e) { return e.streamId; }).when({
                $init: function() { return {}; },
                $deleted: function(s, e) { return s; }
            })
        """, V1));
	}

	[Fact]
	public void V1_deleted_handler_forbidden_with_bistate() {
		Assert.Throws<InvalidProjectionException>(() => new ProjectionSession("""
            options({ biState: true });
            fromAll().when({
                $init: function() { return {}; },
                $initShared: function() { return {}; },
                $deleted: function(s, e) { return s; }
            })
        """, V1));
	}

	// --- biState / shared state ---

	[Fact]
	public void V1_bistate_init_shared_called() {
		using var session = new ProjectionSession("""
            options({ biState: true });
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $initShared: function() { return { total: 0 }; },
                $any: function(s, e) { return s; }
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}", IsJson = true });

		var shared = session.GetSharedState();
		Assert.NotNull(shared);
		Assert.Contains("\"total\":0", shared);
	}

	[Fact]
	public void V1_bistate_handler_receives_array() {
		using var session = new ProjectionSession("""
            options({ biState: true });
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $initShared: function() { return { total: 0 }; },
                Add: function(s, e) { s[0].count++; s[1].total += e.data.amount; return s; }
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Add", StreamId = "s-1", Data = """{"amount":10}""", IsJson = true });

		Assert.Contains("\"count\":1", session.GetState()!);
		Assert.Contains("\"total\":10", session.GetSharedState()!);
	}

	[Fact]
	public void V1_bistate_shared_state_persists_across_partitions() {
		using var session = new ProjectionSession("""
            options({ biState: true });
            fromAll().foreachStream().when({
                $init: function() { return { count: 0 }; },
                $initShared: function() { return { total: 0 }; },
                Add: function(s, e) { s[0].count++; s[1].total += e.data.amount; return s; }
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Add", StreamId = "s-1", Data = """{"amount":5}""", IsJson = true });
		session.Feed(new ProjectionEvent { EventType = "Add", StreamId = "s-2", Data = """{"amount":7}""", IsJson = true });

		Assert.Contains("\"count\":1", session.GetState("s-1")!);
		Assert.Contains("\"count\":1", session.GetState("s-2")!);
		Assert.Contains("\"total\":12", session.GetSharedState()!);
	}

	// --- transformBy / filterBy ---

	[Fact]
	public void V1_transformBy_affects_result() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Add: function(s, e) { s.count++; return s; }
            }).transformBy(function(s) {
                return { total: s.count * 2 };
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Add", StreamId = "s-1", Data = "{}", IsJson = true });
		session.Feed(new ProjectionEvent { EventType = "Add", StreamId = "s-1", Data = "{}", IsJson = true });

		Assert.Contains("\"count\":2", session.GetState()!);
		Assert.Contains("\"total\":4", session.GetResult()!);
	}

	[Fact]
	public void V1_filterBy_excludes_result() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Add: function(s, e) { s.count++; return s; }
            }).filterBy(function(s) {
                return s.count >= 3;
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Add", StreamId = "s-1", Data = "{}", IsJson = true });
		session.Feed(new ProjectionEvent { EventType = "Add", StreamId = "s-1", Data = "{}", IsJson = true });

		Assert.Null(session.GetResult());

		session.Feed(new ProjectionEvent { EventType = "Add", StreamId = "s-1", Data = "{}", IsJson = true });

		Assert.NotNull(session.GetResult());
	}

	[Fact]
	public void V1_chained_transforms() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Add: function(s, e) { s.count++; return s; }
            }).transformBy(function(s) {
                return { doubled: s.count * 2 };
            }).filterBy(function(s) {
                return s.doubled >= 4;
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Add", StreamId = "s-1", Data = "{}", IsJson = true });

		Assert.Null(session.GetResult());

		session.Feed(new ProjectionEvent { EventType = "Add", StreamId = "s-1", Data = "{}", IsJson = true });

		Assert.Contains("\"doubled\":4", session.GetResult()!);
	}

	[Fact]
	public void V1_no_transforms_result_equals_state() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Add: function(s, e) { s.count++; return s; }
            })
        """, V1);

		session.Feed(new ProjectionEvent { EventType = "Add", StreamId = "s-1", Data = "{}", IsJson = true });

		Assert.Equal(session.GetState(), session.GetResult());
	}

	// --- emit / linkTo from handlers ---

	[Fact]
	public void V1_emit_from_handler() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                Ping: function(s, e) {
                    emit("target-stream", "Emitted", { x: 1 });
                    return s;
                }
            })
        """, V1);

		var emitted = new List<EmittedEvent>();
		session.OnEmit = e => emitted.Add(e);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}", IsJson = true });

		Assert.Single(emitted);
		Assert.Equal("target-stream", emitted[0].StreamId);
		Assert.Equal("Emitted", emitted[0].EventType);
		Assert.Contains("\"x\":1", emitted[0].Data!);
		Assert.True(emitted[0].IsJson);
		Assert.False(emitted[0].IsLink);
	}

	[Fact]
	public void V1_linkTo_from_handler() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                Ping: function(s, e) {
                    linkTo("target-stream", e);
                    return s;
                }
            })
        """, V1);

		var emitted = new List<EmittedEvent>();
		session.OnEmit = e => emitted.Add(e);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}", IsJson = true, SequenceNumber = 42 });

		Assert.Single(emitted);
		Assert.Equal("target-stream", emitted[0].StreamId);
		Assert.True(emitted[0].IsLink);
		Assert.Equal("$>", emitted[0].EventType);
	}

	[Fact]
	public void V1_emit_from_created_handler() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return {}; },
                $created: function(s, e) {
                    emit("created-log", "StreamCreated", { stream: e.streamId });
                },
                Ping: function(s, e) { return s; }
            })
        """, V1);

		var emitted = new List<EmittedEvent>();
		session.OnEmit = e => emitted.Add(e);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}", IsJson = true });

		Assert.Contains(emitted, e => e.StreamId == "created-log" && e.EventType == "StreamCreated");
	}

	[Fact]
	public void V1_multiple_emits_per_event() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                Ping: function(s, e) {
                    emit("stream-a", "First", {});
                    emit("stream-b", "Second", {});
                    return s;
                }
            })
        """, V1);

		var emitted = new List<EmittedEvent>();
		session.OnEmit = e => emitted.Add(e);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}", IsJson = true });

		Assert.Equal(2, emitted.Count);
		Assert.Equal("stream-a", emitted[0].StreamId);
		Assert.Equal("First", emitted[0].EventType);
		Assert.Equal("stream-b", emitted[1].StreamId);
		Assert.Equal("Second", emitted[1].EventType);
	}

	// --- Edge cases ---

	[Fact]
	public void V1_category_resolution() {
		using var session = new ProjectionSession("""
            fromCategory("order").when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, V1);

		Assert.NotNull(session.Sources.Categories);
		Assert.Contains("order", session.Sources.Categories);

		session.Feed(new ProjectionEvent { EventType = "Placed", StreamId = "order-123", Data = "{}", IsJson = true });

		Assert.Contains("\"count\":1", session.GetState()!);
	}

	[Fact]
	public void V1_log_callback() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                Ping: function(s, e) {
                    log("hello");
                    return s;
                }
            })
        """, V1);

		var messages = new List<string>();
		session.OnLog = msg => messages.Add(msg);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}", IsJson = true });

		Assert.Single(messages);
		Assert.Equal("hello", messages[0]);
	}

	[Fact]
	public void V1_state_changed_callback() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """, V1);

		var changes = new List<(string partition, string? state)>();
		session.OnStateChanged = (p, s) => changes.Add((p, s));

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}", IsJson = true });

		Assert.Single(changes);
		Assert.Equal("", changes[0].partition);
		Assert.Contains("\"count\":1", changes[0].state!);
	}
}
