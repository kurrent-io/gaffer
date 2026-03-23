using Gaffer.Core.Events;

namespace Gaffer.Core.Tests;

public class BugFixTests
{
    [Fact]
    public void BiState_string_partition_state_serialized_correctly()
    {
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
    public void GetResult_returns_null_for_unknown_partition()
    {
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
    public void Js_error_throws_ProjectionException()
    {
        using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                Bad: function(s, e) { throw "something went wrong"; }
            })
        """);

        var ex = Assert.Throws<ProjectionException>(() =>
            session.Feed(new ProjectionEvent { EventType = "Bad", StreamId = "s-1", Data = "{}" }));

        Assert.Contains("something went wrong", ex.Message);
    }

    [Fact]
    public void Js_type_error_throws_ProjectionException()
    {
        using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                Bad: function(s, e) { s.foo.bar.baz = 1; return s; }
            })
        """);

        Assert.Throws<ProjectionException>(() =>
            session.Feed(new ProjectionEvent { EventType = "Bad", StreamId = "s-1", Data = "{}" }));
    }
}
