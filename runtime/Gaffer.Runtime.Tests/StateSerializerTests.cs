using Gaffer.Runtime.Errors;
using Gaffer.Runtime.Events;

namespace Gaffer.Runtime.Tests;

// The state serializer walks an explicit fixed-size stack. These cover the three ways it used to
// fail with a misleading "Index was outside the bounds of the array" handler error: state nested
// past the stack depth, a circular reference, and a huge sparse array whose length overflows the
// (int) cast (which silently serialized as []). Each should now raise a clean state error. The
// final group covers the serialized-size budget, which also bounds an acyclic shared-reference
// DAG that is tiny in memory but expands exponentially on serialize.
public class StateSerializerTests {
	private static readonly ProjectionEvent TestEvent = new() {
		EventType = "Test",
		StreamId = "s-1",
		SequenceNumber = 1,
		Data = "{}",
		IsJson = true,
	};

	private static ProjectionSession Session(string handlerBody, long? maxStateSizeBytes = null) =>
		new($$"""
			fromAll().when({
				$init: function() { return {}; },
				Test: function(s, e) { {{handlerBody}} }
			})
			""", new ProjectionSessionOptions {
			EngineVersion = ProjectionVersion.V2,
			MaxStateSizeBytes = maxStateSizeBytes ?? ProjectionSessionOptions.DefaultMaxStateSizeBytes,
		});

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

	[Fact]
	public void State_exceeding_configured_size_raises_clean_error() {
		using var session = Session(
			"var s = ''; for (var i = 0; i < 500; i++) s += '0123456789'; return { data: s };",
			maxStateSizeBytes: 1024);

		var ex = Assert.Throws<StateSerializationException>(() => session.Feed(TestEvent));
		Assert.Contains("exceeds the configured maximum", ex.Description, StringComparison.OrdinalIgnoreCase);
		Assert.Contains("1024", ex.Description);
	}

	[Fact]
	public void Shared_reference_dag_explosion_is_bounded() {
		// ~40 objects in memory, but each level shares one child twice, so the graph
		// expands to ~2^40 nodes on serialize. The size budget must stop it (the depth
		// is only 40, well under the stack limit, and there is no cycle to catch).
		using var session = Session("""
			var node = { n: 1 };
			for (var i = 0; i < 40; i++) { node = { x: node, y: node }; }
			return node;
			""", maxStateSizeBytes: 4096);

		var ex = Assert.Throws<StateSerializationException>(() => session.Feed(TestEvent));
		Assert.Contains("exceeds the configured maximum", ex.Description, StringComparison.OrdinalIgnoreCase);
	}

	[Fact]
	public void Flat_array_flood_is_bounded() {
		// A sparse array is tiny in memory but writes one `null` per element in a single
		// serialize step, never returning to the outer loop - so the budget must be checked
		// inside the primitive loop, not only between steps.
		using var session = Session("var a = []; a.length = 2000000000; return a;", maxStateSizeBytes: 4096);

		var ex = Assert.Throws<StateSerializationException>(() => session.Feed(TestEvent));
		Assert.Contains("exceeds the configured maximum", ex.Description, StringComparison.OrdinalIgnoreCase);
	}

	[Fact]
	public void State_within_configured_size_serializes() {
		using var session = Session("return { hello: 'world' };", maxStateSizeBytes: 1024);

		session.Feed(TestEvent);
		Assert.Equal("{\"hello\":\"world\"}", session.GetState());
	}
}
