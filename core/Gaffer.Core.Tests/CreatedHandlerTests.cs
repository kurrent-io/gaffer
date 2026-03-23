using Gaffer.Core.Events;

namespace Gaffer.Core.Tests;

public class CreatedHandlerTests
{
    [Fact]
    public void Created_handler_emits_events()
    {
        var emitted = new List<EmittedEvent>();
        using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return {}; },
                $created: function(s, e) {
                    emit("new-streams", "StreamCreated", { streamId: e.streamId });
                },
                Ping: function(s, e) { return s; }
            })
        """);
        session.OnEmit = e => emitted.Add(e);

        session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "stream-1", Data = "{}" });

        Assert.Single(emitted);
        Assert.Equal("new-streams", emitted[0].StreamId);
        Assert.Equal("StreamCreated", emitted[0].EventType);
        Assert.Contains("stream-1", emitted[0].Data);
    }

    [Fact]
    public void Created_handler_fires_once_per_partition()
    {
        var createdCount = 0;
        using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return {}; },
                $created: function(s, e) {
                    emit("audit", "Created", { partition: e.streamId });
                },
                Ping: function(s, e) { return s; }
            })
        """);
        session.OnEmit = _ => createdCount++;

        session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "stream-1", Data = "{}" });
        session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "stream-1", Data = "{}" });
        session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "stream-2", Data = "{}" });
        session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "stream-1", Data = "{}" });

        // $created fires once for stream-1 and once for stream-2
        Assert.Equal(2, createdCount);
    }

    [Fact]
    public void Created_handler_can_modify_state()
    {
        using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { initialized: false, count: 0 }; },
                $created: function(s, e) {
                    s.initialized = true;
                },
                Ping: function(s, e) { s.count++; return s; }
            })
        """);

        session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

        var state = session.GetState("s-1")!;
        Assert.Contains("\"initialized\":true", state);
        Assert.Contains("\"count\":1", state);
    }
}
