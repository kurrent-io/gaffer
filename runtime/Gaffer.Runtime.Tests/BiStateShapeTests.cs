using Gaffer.Runtime.Errors;
using Gaffer.Runtime.Events;

namespace Gaffer.Runtime.Tests;

// A bi-state handler must keep state as the [state, sharedState] array. Returning anything else is
// persisted and then faults restoring it on the next event, wedging the partition - exactly as
// KurrentDB does. gaffer reproduces that wedge and emits a quirk.biState.nonArrayReturn diagnostic
// so it's diagnosable. These verify the diagnostic fires and that the wedge is reproduced.
public class BiStateShapeTests {
	private const string Source = """
		options({ biState: true });
		fromAll().when({
			$init: function() { return { count: 0 }; },
			$initShared: function() { return { total: 0 }; },
			Bump: function(s, e) { s[0].count++; },
			ReturnScalar: function(s, e) { return 5; },
			ReturnObject: function(s, e) { return { not: "an array" }; }
		})
		""";

	private static ProjectionSession Session() =>
		new(Source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

	private static ProjectionEvent Event(string type, long seq) =>
		new() { EventType = type, StreamId = "s-1", SequenceNumber = seq, Data = "{}", IsJson = true };

	[Theory]
	[InlineData("ReturnScalar")]
	[InlineData("ReturnObject")]
	public void Non_array_return_emits_quirk_diagnostic(string eventType) {
		using var session = Session();

		// KurrentDB persists the malformed value and the event itself succeeds; gaffer reproduces
		// that and flags the impending wedge.
		var result = session.Feed(Event(eventType, 1));
		Assert.Contains(result.Diagnostics, d => d.Code == "quirk.biState.nonArrayReturn");
	}

	[Fact]
	public void Non_array_return_wedges_the_partition_like_kurrentdb() {
		using var session = Session();

		session.Feed(Event("ReturnScalar", 1)); // persists a non-array, as KurrentDB does

		// Restoring the malformed state faults on the next event - the partition is wedged.
		Assert.Throws<ProjectionHandlerException>(() => session.Feed(Event("Bump", 2)));
	}

	[Fact]
	public void Valid_bistate_handler_does_not_warn() {
		using var session = Session();

		var result = session.Feed(Event("Bump", 1));
		Assert.DoesNotContain(result.Diagnostics, d => d.Code == "quirk.biState.nonArrayReturn");
	}
}
