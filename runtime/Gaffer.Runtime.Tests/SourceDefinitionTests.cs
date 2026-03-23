namespace Gaffer.Runtime.Tests;

public class SourceDefinitionTests
{
    [Fact]
    public void FromStreams_multiple()
    {
        using var session = new ProjectionSession("""
            fromStreams(["stream-a", "stream-b"]).when({
                $init: function() { return {}; },
                TestEvent: function(s, e) { return s; }
            })
        """);

        Assert.False(session.Sources.AllStreams);
        Assert.NotNull(session.Sources.Streams);
        Assert.Contains("stream-a", session.Sources.Streams);
        Assert.Contains("stream-b", session.Sources.Streams);
    }

    [Fact]
    public void FromCategory_single()
    {
        using var session = new ProjectionSession("""
            fromCategory("orders").when({
                $init: function() { return {}; },
                TestEvent: function(s, e) { return s; }
            })
        """);

        Assert.NotNull(session.Sources.Categories);
        Assert.Contains("orders", session.Sources.Categories);
    }

    [Fact]
    public void FromAll_foreachStream()
    {
        using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return {}; },
                TestEvent: function(s, e) { return s; }
            })
        """);

        Assert.True(session.Sources.AllStreams);
        Assert.True(session.Sources.ByStreams);
    }

    [Fact]
    public void OutputTo_sets_result_stream()
    {
        using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                TestEvent: function(s, e) { return s; }
            }).outputTo("my-results")
        """);

        Assert.Equal("my-results", session.Sources.ResultStreamName);
    }

    [Fact]
    public void OutputTo_with_partition_pattern()
    {
        using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                TestEvent: function(s, e) { return s; }
            }).outputTo("my-results", "partition-{0}")
        """);

        Assert.Equal("my-results", session.Sources.ResultStreamName);
        Assert.Equal("partition-{0}", session.Sources.PartitionResultStreamNamePattern);
    }

    [Fact]
    public void Options_resultStreamName()
    {
        using var session = new ProjectionSession("""
            options({ resultStreamName: "custom-stream" });
            fromAll().when({
                $init: function() { return {}; },
                TestEvent: function(s, e) { return s; }
            }).outputState()
        """);

        Assert.Equal("custom-stream", session.Sources.ResultStreamName);
    }

    [Fact]
    public void Options_includeLinks()
    {
        using var session = new ProjectionSession("""
            options({ $includeLinks: true });
            fromAll().when({
                $init: function() { return {}; },
                TestEvent: function(s, e) { return s; }
            })
        """);

        Assert.True(session.Sources.IncludeLinks);
    }

    [Fact]
    public void Options_reorderEvents()
    {
        using var session = new ProjectionSession("""
            options({ reorderEvents: true, processingLag: 500 });
            fromStreams(["a", "b"]).when({
                $init: function() { return {}; },
                TestEvent: function(s, e) { return s; }
            })
        """);

        Assert.True(session.Sources.ReorderEvents);
        Assert.Equal(500, session.Sources.ProcessingLag);
    }

    [Fact]
    public void PartitionBy_sets_custom_partitions()
    {
        using var session = new ProjectionSession("""
            fromAll().partitionBy(function(e) { return e.data.key; }).when({
                $init: function() { return {}; },
                TestEvent: function(s, e) { return s; }
            })
        """);

        Assert.True(session.Sources.ByCustomPartitions);
    }

    [Fact]
    public void DefinesFold_set_when_when_used()
    {
        using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                TestEvent: function(s, e) { return s; }
            })
        """);

        Assert.True(session.Sources.DefinesFold);
    }

    [Fact]
    public void OutputState_sets_produces_results()
    {
        using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                TestEvent: function(s, e) { return s; }
            }).outputState()
        """);

        Assert.True(session.Sources.ProducesResults);
    }

    [Fact]
    public void FilterBy_sets_state_transform()
    {
        using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { x: 1 }; },
                TestEvent: function(s, e) { return s; }
            }).filterBy(function(s) { return s.x > 0; }).outputState()
        """);

        Assert.True(session.Sources.DefinesStateTransform);
        Assert.True(session.Sources.ProducesResults);
    }

    [Fact]
    public void Specific_events_listed()
    {
        using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                OrderPlaced: function(s, e) { return s; },
                OrderShipped: function(s, e) { return s; }
            })
        """);

        Assert.False(session.Sources.AllEvents);
        Assert.NotNull(session.Sources.Events);
        Assert.Contains("OrderPlaced", session.Sources.Events);
        Assert.Contains("OrderShipped", session.Sources.Events);
    }

    [Fact]
    public void Any_handler_means_all_events()
    {
        using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                $any: function(s, e) { return s; }
            })
        """);

        Assert.True(session.Sources.AllEvents);
    }
}
