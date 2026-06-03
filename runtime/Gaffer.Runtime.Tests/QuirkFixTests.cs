using Gaffer.Runtime.Errors;
using Gaffer.Runtime.Events;

namespace Gaffer.Runtime.Tests;

public class QuirkFixTests {
	[Fact]
	public void BiState_string_partition_state_matches_upstream_quoting() {
		// Bi-state slots always JSON-encode (matches upstream, all versions), so a
		// raw string in slot 0 persists as "alice" with quotes. This is the correct
		// contract - not a quirk - and is unaffected by quirksVersion.
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
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "SetName", StreamId = "s-1", Data = """{"name":"alice"}""" });

		var state = session.GetState();
		Assert.NotNull(state);
		Assert.Equal("\"alice\"", state);
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
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

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
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

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
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.Throws<ProjectionHandlerException>(() =>
			session.Feed(new ProjectionEvent { EventType = "Bad", StreamId = "s-1", Data = "{}" }));
	}

}
