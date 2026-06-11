using Gaffer.Runtime.Errors;
using Gaffer.Runtime.Events;

namespace Gaffer.Runtime.Tests;

// The state serializer walks an explicit fixed-size stack. These cover the three ways it used to
// fail with a misleading "Index was outside the bounds of the array" handler error: state nested
// past the stack depth, a circular reference, and a huge sparse array whose length overflows the
// (int) cast (which silently serialized as []). Each should now raise a clean state error.
public class StateSerializerTests {
	private static readonly ProjectionEvent TestEvent = new() {
		EventType = "Test",
		StreamId = "s-1",
		SequenceNumber = 1,
		Data = "{}",
		IsJson = true,
	};

	private static ProjectionSession Session(string handlerBody) =>
		new($$"""
			fromAll().when({
				$init: function() { return {}; },
				Test: function(s, e) { {{handlerBody}} }
			})
			""", new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

	[Fact]
	public void Deeply_nested_state_raises_clean_error() {
		using var session = Session("""
			var root = {}; var o = root;
			for (var i = 0; i < 200; i++) { o.child = {}; o = o.child; }
			return root;
			""");

		var ex = Assert.Throws<StateSerializationException>(() => session.Feed(TestEvent));
		Assert.Contains("nested", ex.Description, StringComparison.OrdinalIgnoreCase);
	}

	[Fact]
	public void Circular_state_raises_clean_error() {
		using var session = Session("var o = {}; o.self = o; return o;");

		var ex = Assert.Throws<StateSerializationException>(() => session.Feed(TestEvent));
		Assert.Contains("circular", ex.Description, StringComparison.OrdinalIgnoreCase);
	}

	[Fact]
	public void Oversized_sparse_array_raises_clean_error_not_empty() {
		using var session = Session("var a = []; a.length = 3000000000; return a;");

		var ex = Assert.Throws<StateSerializationException>(() => session.Feed(TestEvent));
		Assert.Contains("array", ex.Description, StringComparison.OrdinalIgnoreCase);
	}

	[Fact]
	public void Deep_but_within_limit_state_still_serializes() {
		using var session = Session("""
			var root = {}; var o = root;
			for (var i = 0; i < 50; i++) { o.child = {}; o = o.child; }
			return root;
			""");

		session.Feed(TestEvent);
		var state = session.GetState();
		Assert.NotNull(state);
		Assert.StartsWith("{\"child\":{\"child\":", state);
	}
}
