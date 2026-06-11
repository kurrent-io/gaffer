using Gaffer.Runtime.Errors;
using Gaffer.Runtime.Events;

namespace Gaffer.Runtime.Tests;

// A host emit/log callback that throws faults the session. A faulted session must refuse further
// work rather than re-enter user JS through the partitionBy / $init / transform entry points.
// These verify the guard fires on the public APIs that reach those entry points (not just
// ProcessEvent, which was already guarded).
public class FaultedSessionTests {
	private static ProjectionEvent Event(long seq = 1) =>
		new() { EventType = "Test", StreamId = "s-1", SequenceNumber = seq, Data = "{}", IsJson = true };

	[Fact]
	public void Faulted_session_does_not_re_run_partitionBy() {
		using var session = new ProjectionSession("""
			fromAll().partitionBy(function(e) { return "p"; }).when({
				$init: function() { return {}; },
				Test: function(s, e) { emit("out", "Ev", {}); return s; }
			})
			""", new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });
		session.OnEmit = _ => throw new Exception("sink down");

		// The emit callback throws, faulting the session.
		Assert.ThrowsAny<Exception>(() => session.Feed(Event()));

		// GetPartitionKey runs partitionBy; on a faulted session it must refuse, not re-run it.
		Assert.Throws<ProjectionHandlerException>(() => session.GetPartitionKey(Event()));
	}

	[Fact]
	public void Faulted_session_refuses_the_result_path() {
		var emitted = new List<EmittedEvent>();
		using var session = new ProjectionSession("""
			fromAll().when({
				$init: function() { return { n: 0 }; },
				Test: function(s, e) { emit("out", "Ev", {}); s.n++; return s; }
			})
			""", new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });
		session.OnEmit = e => emitted.Add(e);

		// First event succeeds and populates partition state.
		session.Feed(Event());
		Assert.Single(emitted);

		// Now the emit callback throws, faulting the session on the next event.
		session.OnEmit = _ => throw new Exception("sink down");
		Assert.ThrowsAny<Exception>(() => session.Feed(Event(2)));

		// The result path reloads state and runs transforms; a faulted session must refuse it.
		Assert.Throws<InvalidOperationException>(() => session.GetResult());
	}
}
