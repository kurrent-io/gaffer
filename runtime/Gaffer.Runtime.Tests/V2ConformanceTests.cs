using Gaffer.Runtime.Errors;
using Gaffer.Runtime.Events;

namespace Gaffer.Runtime.Tests;

public class V2ConformanceTests {
	// -- Event filtering --

	[Fact]
	public void V2_passes_non_json_events_to_handler() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Bin", StreamId = "s-1", Data = "binary", IsJson = false });

		Assert.Contains("\"count\":1", session.GetState()!);
	}

	[Fact]
	public void V2_passes_non_json_event_with_empty_data() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Bin", StreamId = "s-1", Data = null, IsJson = false });

		Assert.Contains("\"count\":1", session.GetState()!);
	}

	[Fact]
	public void V2_skips_json_event_with_null_data() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = null });

		Assert.Contains("\"count\":0", session.GetState()!);
	}

	[Fact]
	public void V2_skips_json_event_with_empty_data() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "" });

		Assert.Contains("\"count\":0", session.GetState()!);
	}

	[Fact]
	public void V2_skips_json_event_with_whitespace_data() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "  " });

		Assert.Contains("\"count\":0", session.GetState()!);
	}

	[Fact]
	public void V2_non_json_whitespace_data_not_skipped() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Bin", StreamId = "s-1", Data = "  ", IsJson = false });

		Assert.Contains("\"count\":1", session.GetState()!);
	}

	// -- Non-JSON event envelope --

	[Fact]
	public void V2_non_json_event_bodyRaw_available() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { raw: "" }; },
                $any: function(s, e) { s.raw = e.bodyRaw; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Bin", StreamId = "s-1", Data = "hello", IsJson = false });

		Assert.Contains("\"raw\":\"hello\"", session.GetState()!);
	}

	[Fact]
	public void V2_non_json_event_body_is_undefined() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { undef: false }; },
                $any: function(s, e) { s.undef = (typeof e.body === 'undefined'); return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Bin", StreamId = "s-1", Data = "hello", IsJson = false });

		Assert.Contains("\"undef\":true", session.GetState()!);
	}

	// -- System events --

	[Fact]
	public void V2_hard_delete_triggers_deleted_handler() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { deleted: false }; },
                Order: function(s, e) { return s; },
                $deleted: function(s, e) { s.deleted = true; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Order", StreamId = "order-1", Data = "{}" });
		session.Feed(new ProjectionEvent {
			EventType = ProjectionSession.StreamDeletedEventType,
			StreamId = "order-1",
			Data = "{}",
		});

		Assert.Contains("\"deleted\":true", session.GetState("order-1")!);
	}

	[Fact]
	public void V2_soft_delete_triggers_deleted_handler() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { deleted: false }; },
                Order: function(s, e) { return s; },
                $deleted: function(s, e) { s.deleted = true; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Order", StreamId = "order-1", Data = "{}" });
		session.Feed(new ProjectionEvent {
			EventType = "$metadata",
			StreamId = "$$order-1",
			Data = """{"$tb":9223372036854775807}""",
		});

		Assert.Contains("\"deleted\":true", session.GetState("order-1")!);
	}

	[Fact]
	public void V2_metadata_without_tb_is_normal_event() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent {
			EventType = "$metadata",
			StreamId = "$$stream-1",
			Data = """{"$maxAge":100}""",
		});

		Assert.Contains("\"count\":1", session.GetState()!);
	}

	[Fact]
	public void V2_malformed_metadata_throws() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return {}; },
                Order: function(s, e) { return s; },
                $deleted: function(s, e) { s.deleted = true; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Order", StreamId = "stream-1", Data = "{}" });

		Assert.Throws<MalformedEventException>(() =>
			session.Feed(new ProjectionEvent {
				EventType = "$metadata",
				StreamId = "$$stream-1",
				Data = "not json",
			}));
	}

	// -- Null state (V2-specific) --

	[Fact]
	public void V2_null_state_preserved_on_revisit() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { from: "init" }; },
                Tag: function(s, e) { return { from: "tag" }; },
                Clear: function(s, e) { return null; },
                Probe: function(s, e) { return { saw: JSON.stringify(s) }; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Tag", StreamId = "s-1", Data = "{}" });
		Assert.Contains("\"from\":\"tag\"", session.GetState("s-1")!);

		session.Feed(new ProjectionEvent { EventType = "Clear", StreamId = "s-1", Data = "{}" });
		Assert.Null(session.GetState("s-1"));

		session.Feed(new ProjectionEvent { EventType = "Probe", StreamId = "s-1", Data = "{}" });
		Assert.Contains("\"saw\":\"null\"", session.GetState("s-1")!);
	}

	[Fact]
	public void V2_undefined_return_preserves_mutated_state() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Increment: function(s, e) { s.count++; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Increment", StreamId = "s-1", Data = "{}" });

		Assert.Contains("\"count\":1", session.GetState()!);
	}

	// -- Partition lifecycle --

	[Fact]
	public void V2_created_handler_called_on_first_partition_visit() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { created: false }; },
                $created: function(s, e) { s.created = true; },
                Ping: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Contains("\"created\":true", session.GetState("s-1")!);
	}

	[Fact]
	public void V2_created_handler_not_called_on_revisit() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { createdCount: 0 }; },
                $created: function(s, e) { s.createdCount++; },
                Ping: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });
		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Contains("\"createdCount\":1", session.GetState("s-1")!);
	}

	[Fact]
	public void V2_partitionBy_null_skips_event() {
		using var session = new ProjectionSession("""
            fromAll().partitionBy(function(e) {
                if (e.eventType === "Skip") return null;
                return e.streamId;
            }).when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Skip", StreamId = "s-1", Data = "{}" });

		// KurrentDB skips events when partitionBy returns null.
		// No partition should be initialized, no state anywhere.
		Assert.Null(session.GetState("s-1"));
		Assert.Null(session.GetState(""));
	}

	[Fact]
	public void V2_partitionBy_number_converted_to_string() {
		using var session = new ProjectionSession("""
            fromAll().partitionBy(function(e) {
                return 42;
            }).when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Contains("\"count\":1", session.GetState("42")!);
	}

	// -- Event type filtering --

	[Fact]
	public void V2_specific_handlers_filter_unmatched_events() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                A: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "B", StreamId = "s-1", Data = "{}" });

		Assert.Null(session.GetState());
	}

	[Fact]
	public void V2_any_handler_catches_unmatched_events() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { aCount: 0, anyCount: 0 }; },
                A: function(s, e) { s.aCount++; return s; },
                $any: function(s, e) { s.anyCount++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "B", StreamId = "s-1", Data = "{}" });

		var state = session.GetState()!;
		Assert.Contains("\"aCount\":0", state);
		Assert.Contains("\"anyCount\":1", state);
	}

	// -- $deleted constraints --

	[Fact]
	public void V2_deleted_handler_forbidden_with_bistate() {
		Assert.Throws<InvalidProjectionException>(() => new ProjectionSession("""
            options({ biState: true });
            fromAll().when({
                $init: function() { return {}; },
                $initShared: function() { return {}; },
                $deleted: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 }));
	}

	// -- biState / shared state --

	[Fact]
	public void V2_bistate_init_shared_called() {
		using var session = new ProjectionSession("""
            options({ biState: true });
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $initShared: function() { return { total: 0 }; },
                Ping: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		var shared = session.GetSharedState();
		Assert.NotNull(shared);
		Assert.Contains("\"total\":0", shared);
	}

	[Fact]
	public void V2_bistate_handler_receives_array() {
		using var session = new ProjectionSession("""
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
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Added", StreamId = "s-1", Data = """{"amount":10}""" });

		Assert.Contains("\"count\":1", session.GetState()!);
		Assert.Contains("\"total\":10", session.GetSharedState()!);
	}

	[Fact]
	public void V2_bistate_shared_state_persists_across_partitions() {
		using var session = new ProjectionSession("""
            options({ biState: true });
            fromAll().foreachStream().when({
                $init: function() { return { count: 0 }; },
                $initShared: function() { return { total: 0 }; },
                Added: function(s, e) {
                    s[0].count++;
                    s[1].total += e.data.amount;
                    return s;
                }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Added", StreamId = "s-1", Data = """{"amount":5}""" });
		session.Feed(new ProjectionEvent { EventType = "Added", StreamId = "s-2", Data = """{"amount":7}""" });

		Assert.Contains("\"count\":1", session.GetState("s-1")!);
		Assert.Contains("\"count\":1", session.GetState("s-2")!);
		Assert.Contains("\"total\":12", session.GetSharedState()!);
	}

	[Fact]
	public void V2_bistate_null_shared_state_preserved() {
		using var session = new ProjectionSession("""
            options({ biState: true });
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $initShared: function() { return { total: 0 }; },
                Reset: function(s, e) {
                    s[1] = null;
                    return s;
                }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Reset", StreamId = "s-1", Data = "{}" });

		Assert.Null(session.GetSharedState());
	}

	// -- transformBy / filterBy / outputState --
	//
	// V2 doesn't iterate _transforms; result == post-handler state. JS calls
	// to transformBy / filterBy / outputState succeed silently (matches
	// upstream V2's JintProjectionStateHandler which still registers them),
	// but the engine never invokes them on events. See
	// cli/internal/mcpserver/resources/v1-v2-differences.md.

	[Fact]
	public void V2_transformBy_not_applied() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            }).transformBy(function(s) {
                return { total: s.count * 2 };
            }).outputState()
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });
		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		// State == result; transform never ran.
		Assert.Equal(session.GetState(), session.GetResult());
		Assert.Contains("\"count\":2", session.GetResult()!);
		Assert.DoesNotContain("total", session.GetResult()!);
	}

	[Fact]
	public void V2_filterBy_not_applied() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            }).filterBy(function(s) {
                return s.count >= 3;
            }).outputState()
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		// In V1 this would filter out the result until count >= 3. In V2 the
		// filter never runs, so result == state from the very first event.
		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Equal(session.GetState(), session.GetResult());
		Assert.Contains("\"count\":1", session.GetResult()!);
	}

	[Fact]
	public void V2_chained_transforms_not_applied() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            }).transformBy(function(s) {
                return { doubled: s.count * 2, active: true };
            }).filterBy(function(s) {
                return s.active;
            }).outputState()
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		// Neither transform nor filter run; result is the post-handler state.
		Assert.Equal(session.GetState(), session.GetResult());
		Assert.Contains("\"count\":1", session.GetResult()!);
	}

	[Fact]
	public void V2_transform_throw_never_surfaces() {
		// In V1 this would surface as ProjectionTransformException. V2 never
		// invokes the transform, so the quirky JS body is dead code.
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                Ping: function(s, e) { return s; }
            }).transformBy(function(s) {
                throw new Error("transform failed");
            }).outputState()
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Equal(session.GetState(), session.GetResult());
	}

	[Fact]
	public void V2_no_transforms_result_equals_state() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Equal(session.GetState(), session.GetResult());
	}

	[Fact]
	public void V2_biState_result_is_partition_state_only() {
		// V2's PartitionProcessor writes the partition slot to the result
		// stream (not the [partition, shared] array). Result must equal
		// the partition slot, matching what GetState() returns - even when
		// transformBy is registered, since V2 never invokes it.
		using var session = new ProjectionSession("""
            options({ biState: true });
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $initShared: function() { return { total: 0 }; },
                Ping: function(s, e) { s[0].count++; s[1].total++; return s; }
            }).transformBy(function(s) {
                return { wrong: true };
            }).outputState()
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		// Result == partition state, not the array, not the transform output.
		Assert.Equal(session.GetState(), session.GetResult());
		Assert.Contains("\"count\":1", session.GetResult()!);
		Assert.DoesNotContain("total", session.GetResult()!);
		Assert.DoesNotContain("wrong", session.GetResult()!);
	}

	[Fact]
	public void V2_stringState_feedResultStateEqualsResult() {
		// V2 conformance promise: result == post-handler state. A bare non-JSON string
		// trips quirk.serialize.rawString, so gaffer JSON-encodes it (safe) on both the
		// state and result paths - they must still match.
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return "alice"; },
                Ping: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var feedResult = session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Equal("\"alice\"", feedResult.State);
		Assert.Equal(feedResult.State, feedResult.Result);
	}

	[Fact]
	public void V2_biState_stringPartitionSlot_feedResultStateEqualsResult() {
		// Same invariant under bi-state: the slot-0 conversion in
		// PrepareOutput (always JSON-encoding the slot) must be reflected
		// in the V2 result.
		using var session = new ProjectionSession("""
            options({ biState: true });
            fromAll().when({
                $init: function() { return "initial"; },
                $initShared: function() { return {}; },
                Ping: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var feedResult = session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Equal(feedResult.State, feedResult.Result);
	}

	[Fact]
	public void V2_biState_nullPartitionSlot_returnsNull() {
		// Upstream V2's PartitionProcessor skips emission when newState is
		// null (`if (newState != null)` at PartitionProcessor.cs:147).
		// Mirror that: a null partition slot must surface as C# null, not
		// as the JSON literal "null".
		using var session = new ProjectionSession("""
            options({ biState: true });
            fromAll().when({
                $init: function() { return null; },
                $initShared: function() { return {}; },
                Ping: function(s, e) { return [null, s[1]]; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Null(session.GetResult());
	}

	[Fact]
	public void V2_defines_state_transform_without_transform_function() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            }).$defines_state_transform()
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.True(session.Sources.DefinesStateTransform);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		var result = session.GetResult();
		Assert.NotNull(result);
		Assert.Contains("\"count\":1", result);
	}

	// -- emit / linkTo from handlers --

	[Fact]
	public void V2_emit_from_handler() {
		var emitted = new List<EmittedEvent>();
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                Order: function(s, e) {
                    emit("target-stream", "Emitted", { x: 1 });
                    return s;
                }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });
		session.OnEmit = e => emitted.Add(e);

		session.Feed(new ProjectionEvent { EventType = "Order", StreamId = "s-1", Data = "{}" });

		Assert.Single(emitted);
		Assert.Equal("target-stream", emitted[0].StreamId);
		Assert.Equal("Emitted", emitted[0].EventType);
		Assert.Contains("\"x\":1", emitted[0].Data!);
		Assert.False(emitted[0].IsLink);
	}

	[Fact]
	public void V2_linkTo_from_handler() {
		var emitted = new List<EmittedEvent>();
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                Order: function(s, e) {
                    linkTo("target-stream", e);
                    return s;
                }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });
		session.OnEmit = e => emitted.Add(e);

		session.Feed(new ProjectionEvent { EventType = "Order", StreamId = "s-1", Data = "{}" });

		Assert.Single(emitted);
		Assert.Equal("target-stream", emitted[0].StreamId);
		Assert.True(emitted[0].IsLink);
	}

	[Fact]
	public void V2_emit_from_created_handler() {
		var emitted = new List<EmittedEvent>();
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return {}; },
                $created: function(s, e) {
                    emit("created-stream", "StreamCreated", { stream: e.streamId });
                },
                Ping: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });
		session.OnEmit = e => emitted.Add(e);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Single(emitted);
		Assert.Equal("created-stream", emitted[0].StreamId);
		Assert.Equal("StreamCreated", emitted[0].EventType);
	}

	[Fact]
	public void V2_multiple_emits_per_event() {
		var emitted = new List<EmittedEvent>();
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
		session.OnEmit = e => emitted.Add(e);

		session.Feed(new ProjectionEvent { EventType = "Order", StreamId = "s-1", Data = "{}" });

		Assert.Equal(2, emitted.Count);
		Assert.Equal("stream-a", emitted[0].StreamId);
		Assert.Equal("stream-b", emitted[1].StreamId);
	}

	// -- Non-JSON specifics (V2 only) --

	[Fact]
	public void V2_non_json_event_does_not_trigger_emit() {
		var emitted = new List<EmittedEvent>();
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                $any: function(s, e) {
                    if (e.isJson) emit("target", "Forwarded", {});
                    return s;
                }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });
		session.OnEmit = e => emitted.Add(e);

		session.Feed(new ProjectionEvent { EventType = "Bin", StreamId = "s-1", Data = "raw data", IsJson = false });

		Assert.Empty(emitted);
	}

	[Fact]
	public void V2_non_json_event_with_data_bodyRaw_in_state() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { raw: "" }; },
                $any: function(s, e) { s.raw = e.bodyRaw; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Bin", StreamId = "s-1", Data = "raw binary data", IsJson = false });

		Assert.Contains("\"raw\":\"raw binary data\"", session.GetState()!);
	}

	// -- Edge cases --

	[Fact]
	public void V2_category_resolution() {
		using var session = new ProjectionSession("""
            fromCategory("order").when({
                $init: function() { return {}; },
                Placed: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.NotNull(session.Sources.Categories);
		Assert.Contains("order", session.Sources.Categories);
	}

	[Fact]
	public void V2_log_callback() {
		var logs = new List<string>();
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                Ping: function(s, e) { log("hello"); return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });
		session.OnLog = msg => logs.Add(msg);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Single(logs);
		Assert.Equal("hello", logs[0]);
	}

	[Fact]
	public void V2_state_changed_callback() {
		var changes = new List<(string partition, string? state)>();
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });
		session.OnStateChanged = (p, s) => changes.Add((p, s));

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Single(changes);
		Assert.Contains("\"count\":1", changes[0].state!);
	}

	[Fact]
	public void V2_set_state_and_continue() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.SetState(null, """{"count":10}""");
		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		Assert.Contains("\"count\":11", session.GetState()!);
	}

	[Fact]
	public void V2_get_partition_key() {
		using var session = new ProjectionSession("""
            fromAll().partitionBy(function(e) {
                return e.body.region;
            }).when({
                $init: function() { return {}; },
                Order: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		var key = session.GetPartitionKey(new ProjectionEvent {
			EventType = "Order",
			StreamId = "order-1",
			Data = """{"region":"us-east"}""",
		});

		Assert.Equal("us-east", key);
	}

	// -- C API edge cases --

	[Fact]
	public void ParseEvent_json_null_data_becomes_csharp_null() {
		var json = """{"eventType":"Ping","streamId":"s-1","sequenceNumber":0,"isJson":true,"data":null,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}""";
		var evt = NativeExports.ParseEvent(json);
		Assert.Null(evt.Data);
	}

	// -- SetState edge cases --

	[Fact]
	public void SetState_empty_string_throws() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.Throws<Errors.InvalidArgumentException>(() => session.SetState(null, ""));
	}

	// -- Null state preserved (V2-specific) --

	[Fact]
	public void V2_unpartitioned_null_state_preserved() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; },
                Clear: function(s, e) { return null; },
                Probe: function(s, e) { return { saw: JSON.stringify(s) }; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });
		session.Feed(new ProjectionEvent { EventType = "Clear", StreamId = "s-1", Data = "{}" });

		Assert.Null(session.GetState());

		session.Feed(new ProjectionEvent { EventType = "Probe", StreamId = "s-1", Data = "{}" });

		Assert.Contains("\"saw\":\"null\"", session.GetState()!);
	}

	[Fact]
	public void V2_set_state_then_handler_returns_null_preserved() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Clear: function(s, e) { return null; },
                Probe: function(s, e) { return { saw: JSON.stringify(s) }; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.SetState(null, """{"count":5}""");
		session.Feed(new ProjectionEvent { EventType = "Clear", StreamId = "s-1", Data = "{}" });

		Assert.Null(session.GetState());

		session.Feed(new ProjectionEvent { EventType = "Probe", StreamId = "s-1", Data = "{}" });

		Assert.Contains("\"saw\":\"null\"", session.GetState()!);
	}

	// -- Partition edge cases --

	[Fact]
	public void V2_deleted_on_unseen_partition_gets_init_state() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { deleted: false }; },
                Ping: function(s, e) { return s; },
                $deleted: function(s, e) { s.deleted = true; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent {
			EventType = "$streamDeleted",
			StreamId = "order-1",
			Data = "{}",
		});

		Assert.Contains("\"deleted\":true", session.GetState("order-1")!);
	}

	[Fact]
	public void V2_get_result_unknown_partition_returns_null() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.Null(session.GetResult("nonexistent"));
	}

	// -- Link event filtering --

	[Fact]
	public void V2_resolved_link_events_filtered_by_default() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent {
			EventType = "OrderPlaced",
			StreamId = "order-1",
			Data = "{}",
			LinkMetadata = """{"$o":"order-1"}""",
		});

		Assert.Null(session.GetState());
	}

	[Fact]
	public void V2_resolved_link_events_passed_when_includeLinks_true() {
		using var session = new ProjectionSession("""
            options({ $includeLinks: true });
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent {
			EventType = "OrderPlaced",
			StreamId = "order-1",
			Data = "{}",
			LinkMetadata = """{"$o":"order-1"}""",
		});

		Assert.Contains("\"count\":1", session.GetState()!);
	}

	[Fact]
	public void V2_raw_link_events_filtered_by_default() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent {
			EventType = "$>",
			StreamId = "$ce-order",
			Data = "0@order-1",
			IsJson = false,
		});

		Assert.Null(session.GetState());
	}

	[Fact]
	public void V2_raw_link_events_passed_when_includeLinks_true() {
		using var session = new ProjectionSession("""
            options({ $includeLinks: true });
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent {
			EventType = "$>",
			StreamId = "$ce-order",
			Data = "0@order-1",
			IsJson = false,
		});

		Assert.Contains("\"count\":1", session.GetState()!);
	}
}
