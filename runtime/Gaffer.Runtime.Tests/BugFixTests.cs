using Gaffer.Runtime.Errors;
using Gaffer.Runtime.Events;

namespace Gaffer.Runtime.Tests;

public class BugFixTests {
	[Fact]
	public void BiState_string_partition_state_serialized_correctly() {
		using var session = new ProjectionSession("""
            options({ biState: true });
            fromAll().when({
                $init: function() { return "initial"; },
                $initShared: function() { return { count: 0 }; },
                SetName: function(s, e) {
                    s[0] = e.data.name;
                    s[1].count++;
                    return s;
                }
            })
        """);

		session.Feed(new ProjectionEvent { EventType = "SetName", StreamId = "s-1", Data = """{"name":"alice"}""" });

		var state = session.GetState();
		Assert.NotNull(state);
		Assert.Equal("alice", state);
	}

	[Fact]
	public void GetResult_returns_null_for_unknown_partition() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            }).transformBy(function(s) {
                return { total: s.count };
            }).outputState()
        """);

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "known", Data = "{}" });

		Assert.NotNull(session.GetResult("known"));
		Assert.Null(session.GetResult("unknown"));
	}

	[Fact]
	public void Js_error_throws_ProjectionHandlerException() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                Bad: function(s, e) { throw "something went wrong"; }
            })
        """);

		var ex = Assert.Throws<ProjectionHandlerException>(() =>
			session.Feed(new ProjectionEvent { EventType = "Bad", StreamId = "s-1", Data = "{}" }));

		Assert.Contains("something went wrong", ex.Description);
	}

	[Fact]
	public void Js_type_error_throws_ProjectionHandlerException() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                Bad: function(s, e) { s.foo.bar.baz = 1; return s; }
            })
        """);

		Assert.Throws<ProjectionHandlerException>(() =>
			session.Feed(new ProjectionEvent { EventType = "Bad", StreamId = "s-1", Data = "{}" }));
	}

	[Fact]
	public void SerializeGafferError_truncates_when_requested() {
		var longDescription = new string('x', 300);
		var ex = new ProjectionHandlerException(
			longDescription, "Test", "s-1", 0, "p-1",
			jsStack: "at foo (<anonymous>:1:1)\nat bar (<anonymous>:2:1)");

		var full = NativeExports.SerializeGafferError(ex);
		var truncated = NativeExports.SerializeGafferError(ex, truncate: true);

		Assert.Contains("jsStack", full);
		Assert.DoesNotContain("jsStack", truncated);
		Assert.Contains("xxx...", truncated);
		Assert.True(truncated.Length < full.Length);

		using var doc = System.Text.Json.JsonDocument.Parse(truncated);
		Assert.Equal("handler-error", doc.RootElement.GetProperty("code").GetString());
		Assert.EndsWith("...", doc.RootElement.GetProperty("description").GetString());
	}
}
