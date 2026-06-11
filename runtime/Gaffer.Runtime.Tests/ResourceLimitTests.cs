using Gaffer.Runtime.Errors;
using Gaffer.Runtime.Events;

namespace Gaffer.Runtime.Tests;

// These exercise the Jint sandbox guardrails (recursion / stack / memory / JSON depth).
// A regression here doesn't just fail an assertion - an unguarded runaway raises an
// uncatchable StackOverflowException or OOMs, which crashes the whole test process.
// Each test asserting a clean throw IS the guard against that.
public class ResourceLimitTests {
	private static readonly ProjectionEvent TestEvent = new() {
		EventType = "Test",
		StreamId = "s-1",
		SequenceNumber = 1,
		Data = "{}",
		IsJson = true,
	};

	private static ProjectionSession Session(string source, bool debug = false) =>
		new(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = debug });

	[Fact]
	public void Runaway_recursion_throws_instead_of_crashing() {
		using var session = Session("""
			fromAll().when({
				$init: function() { return {}; },
				Test: function(s, e) { function f() { return f(); } return f(); }
			})
			""");

		var ex = Assert.Throws<ProjectionHandlerException>(() => session.Feed(TestEvent));
		Assert.Contains("call stack", ex.Description, StringComparison.OrdinalIgnoreCase);
	}

	[Fact]
	public void Mutual_recursion_throws_instead_of_crashing() {
		using var session = Session("""
			fromAll().when({
				$init: function() { return {}; },
				Test: function(s, e) {
					function a() { return b(); }
					function b() { return a(); }
					return a();
				}
			})
			""");

		var ex = Assert.Throws<ProjectionHandlerException>(() => session.Feed(TestEvent));
		Assert.Contains("call stack", ex.Description, StringComparison.OrdinalIgnoreCase);
	}

	[Fact]
	public void Runaway_recursion_throws_in_debug_mode() {
		// The time constraint is disabled under debug, so the stack guard is the only thing
		// standing between a runaway and a host-killing StackOverflow. It must stay active.
		using var session = Session("""
			fromAll().when({
				$init: function() { return {}; },
				Test: function(s, e) { function f() { return f(); } return f(); }
			})
			""", debug: true);

		var ex = Assert.Throws<ProjectionHandlerException>(() => session.Feed(TestEvent));
		Assert.Contains("call stack", ex.Description, StringComparison.OrdinalIgnoreCase);
	}

	[Fact]
	public void Bounded_recursion_still_works() {
		using var session = Session("""
			fromAll().when({
				$init: function() { return { total: 0 }; },
				Test: function(s, e) {
					function sum(n) { return n === 0 ? 0 : n + sum(n - 1); }
					s.total = sum(100);
					return s;
				}
			})
			""");

		session.Feed(TestEvent);
		Assert.Contains("\"total\":5050", session.GetResult());
	}

	[Fact]
	public void Memory_limit_exceeded_throws_instead_of_oom() {
		using var session = Session("""
			fromAll().when({
				$init: function() { return {}; },
				Test: function(s, e) {
					var chunks = [];
					while (true) { chunks.push('x'.repeat(1000000)); }
				}
			})
			""");

		// The Jint MemoryLimitExceededException surfaces through the standard catch-all with
		// its raw "allocated ... but is limited to ..." message (no custom masking).
		var ex = Assert.Throws<ProjectionHandlerException>(() => session.Feed(TestEvent));
		Assert.Contains("limited to", ex.Description, StringComparison.OrdinalIgnoreCase);
	}

	[Fact]
	public void Deeply_nested_json_event_data_throws_malformed_not_crash() {
		using var session = Session("""
			fromAll().when({
				$init: function() { return {}; },
				Test: function(s, e) { return e.body; }
			})
			""");

		var deeplyNested = new string('[', 100_000);
		Assert.Throws<MalformedEventException>(() => session.Feed(new ProjectionEvent {
			EventType = "Test",
			StreamId = "s-1",
			SequenceNumber = 1,
			Data = deeplyNested,
			IsJson = true,
		}));
	}
}
