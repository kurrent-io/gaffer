using Gaffer.Runtime.Events;

namespace Gaffer.Runtime.Tests;

/// <summary>
/// Conformance fixtures mirroring upstream KurrentDB-projections-v2 tests.
/// Locks in semantics gaffer matches today so future runtime changes can't
/// silently drift away from upstream behaviour.
/// <para>
/// Intentionally omitted from upstream's <c>DeletedStreamCategoryProjectionTests</c>:
/// the <c>v2_category_tombstone_after_events_indexed</c> variant (depends on
/// "events indexed before projection starts" - no analogue at runtime-unit
/// level) and the recovery scenarios (stop / restart / replay from checkpoint
/// are engine-orchestration concerns, not runtime).
/// </para>
/// </summary>
public class ConformanceFixturesTests {
	private static ProjectionSessionOptions V2Options => new() { EngineVersion = ProjectionVersion.V2 };

	private static ProjectionEvent DeleteEvent(string streamId) => new() {
		EventType = ProjectionSession.StreamDeletedEventType,
		StreamId = streamId,
		Data = "{}",
	};

	private static ProjectionEvent Event(string type, string streamId) => new() {
		EventType = type,
		StreamId = streamId,
		Data = "{}",
		IsJson = true,
	};

	// -- DB-2034: undeclared event types are skipped --
	// Mirrors upstream "filter events by declared type" (DB-2034). Gaffer
	// is already correct; this is an explicit assertion locking in the
	// behaviour.

	[Fact]
	public void DB_2034_UndeclaredEventType_IsSkipped() {
		using var session = new ProjectionSession("""
			fromAll().when({
				$init: function () { return { foos: 0, bars: 0 }; },
				Foo: function (s, e) { s.foos++; return s; },
				Bar: function (s, e) { s.bars++; return s; }
			});
		""", V2Options);

		var fooResult = session.Feed(Event("Foo", "s-1"));
		var bazResult = session.Feed(Event("Baz", "s-1"));
		var barResult = session.Feed(Event("Bar", "s-1"));

		Assert.Equal(FeedStatus.Processed, fooResult.Status);
		Assert.Equal(FeedStatus.Skipped, bazResult.Status);
		Assert.Equal("unhandled", bazResult.SkipReason);
		Assert.Equal(FeedStatus.Processed, barResult.Status);

		// Projection state reflects only declared types.
		var state = session.GetState();
		Assert.NotNull(state);
		Assert.Contains("\"foos\":1", state);
		Assert.Contains("\"bars\":1", state);
	}

	// -- fromCategory + foreachStream + $deleted --
	// Mirrors upstream DeletedStreamCategoryProjectionTests. Integration
	// scenarios there exercise tombstone-while-running, tombstone-then-events,
	// and recovery; we replicate the per-stream state-mutation invariants
	// at the runtime-unit level. The IncrementSource / SetSource projection
	// shapes are copied verbatim from upstream.

	private const string IncrementSource = """
		fromCategory('cart').foreachStream().when({
			$init: function () { return { a: 0 }; },
			type1: function (s, e) { s.a++; return s; },
			type2: function (s, e) { s.a++; return s; },
			$deleted: function (s, e) { s.deleted = 1; return s; }
		}).outputState();
	""";

	[Fact]
	public void Category_ForeachStream_Deleted_AppliesPerStream() {
		// Scenario mirrors v2_category_tombstone_while_projection_running:
		// events accrue per stream, then one stream is tombstoned.
		using var session = new ProjectionSession(IncrementSource, V2Options);

		session.Feed(Event("type1", "cart-1"));
		session.Feed(Event("type2", "cart-1"));
		session.Feed(Event("type1", "cart-2"));
		session.Feed(Event("type2", "cart-2"));

		// Tombstone cart-1.
		session.Feed(DeleteEvent("cart-1"));

		var cart1 = session.GetState("cart-1");
		var cart2 = session.GetState("cart-2");
		Assert.NotNull(cart1);
		Assert.NotNull(cart2);
		Assert.Contains("\"a\":2", cart1);
		Assert.Contains("\"deleted\":1", cart1);
		// cart-2 is unaffected by cart-1's tombstone.
		Assert.Contains("\"a\":2", cart2);
		Assert.DoesNotContain("\"deleted\"", cart2);
	}

	[Fact]
	public void Category_ForeachStream_Deleted_ThenMoreEvents() {
		// Scenario mirrors v2_category_tombstone_while_running_with_more_events:
		// events accrue, one stream is tombstoned, then more events arrive
		// on different streams. Only the tombstoned stream carries the deleted
		// flag; further activity in other streams keeps incrementing.
		using var session = new ProjectionSession(IncrementSource, V2Options);

		session.Feed(Event("type1", "cart-1"));
		session.Feed(Event("type2", "cart-1"));
		session.Feed(Event("type1", "cart-2"));
		session.Feed(Event("type2", "cart-2"));
		session.Feed(DeleteEvent("cart-1"));
		session.Feed(Event("type1", "cart-2"));
		session.Feed(Event("type2", "cart-2"));
		session.Feed(Event("type1", "cart-3"));

		Assert.Contains("\"a\":2", session.GetState("cart-1")!);
		Assert.Contains("\"deleted\":1", session.GetState("cart-1")!);
		Assert.Contains("\"a\":4", session.GetState("cart-2")!);
		Assert.Contains("\"a\":1", session.GetState("cart-3")!);
	}

	[Fact]
	public void Category_ForeachStream_Deleted_OnUnseenStream() {
		// Mirrors v2_category_tombstone_for_stream_with_events_written_after_indexing:
		// a tombstone for a stream the projection hasn't seen yet still triggers
		// the $deleted handler against an init'd state.
		using var session = new ProjectionSession(IncrementSource, V2Options);

		session.Feed(Event("type1", "cart-1"));
		session.Feed(DeleteEvent("cart-2")); // never had any type1/type2 events

		Assert.Contains("\"a\":1", session.GetState("cart-1")!);
		// cart-2 only has the $init + $deleted side effect.
		var cart2 = session.GetState("cart-2");
		Assert.NotNull(cart2);
		Assert.Contains("\"deleted\":1", cart2);
	}
}
