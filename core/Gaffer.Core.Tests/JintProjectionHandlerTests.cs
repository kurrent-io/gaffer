using Gaffer.Core.Events;
using Gaffer.Core.Projection;

namespace Gaffer.Core.Tests;

public class JintProjectionHandlerTests
{
    private static JintProjectionHandler CreateHandler(string source) =>
        new(source, enableContentTypeValidation: false, TimeSpan.FromSeconds(5), TimeSpan.FromSeconds(5));

    [Fact]
    public void Simple_count_projection()
    {
        using var handler = CreateHandler("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                ItemAdded: function(state, event) { state.count++; return state; }
            })
        """);

        handler.Initialize();
        handler.ProcessEvent("", "", new ProjectionEvent
        {
            EventType = "ItemAdded",
            StreamId = "cart-1",
            Data = "{}",
        }, out var state, out _, out _);

        Assert.NotNull(state);
        Assert.Contains("\"count\":1", state);
    }

    [Fact]
    public void Multiple_events_accumulate_state()
    {
        using var handler = CreateHandler("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                ItemAdded: function(state, event) { state.count++; return state; }
            })
        """);

        handler.Initialize();
        for (var i = 0; i < 5; i++)
        {
            handler.ProcessEvent("", "", new ProjectionEvent
            {
                EventType = "ItemAdded",
                StreamId = "cart-1",
                Data = "{}",
                SequenceNumber = i,
            }, out _, out _, out _);
        }

        handler.ProcessEvent("", "", new ProjectionEvent
        {
            EventType = "ItemAdded",
            StreamId = "cart-1",
            Data = "{}",
            SequenceNumber = 5,
        }, out var state, out _, out _);

        Assert.Contains("\"count\":6", state);
    }

    [Fact]
    public void Event_data_is_accessible()
    {
        using var handler = CreateHandler("""
            fromAll().when({
                $init: function() { return { total: 0 }; },
                Deposited: function(state, event) {
                    state.total += event.data.amount;
                    return state;
                }
            })
        """);

        handler.Initialize();
        handler.ProcessEvent("", "", new ProjectionEvent
        {
            EventType = "Deposited",
            StreamId = "account-1",
            Data = """{"amount": 100}""",
        }, out var state, out _, out _);

        Assert.Contains("\"total\":100", state);
    }

    [Fact]
    public void Emit_produces_emitted_events()
    {
        using var handler = CreateHandler("""
            fromAll().when({
                $init: function() { return {}; },
                OrderPlaced: function(state, event) {
                    emit("notifications", "OrderNotification", { orderId: event.data.orderId });
                    return state;
                }
            })
        """);

        handler.Initialize();
        handler.ProcessEvent("", "", new ProjectionEvent
        {
            EventType = "OrderPlaced",
            StreamId = "order-1",
            Data = """{"orderId": "ABC123"}""",
        }, out _, out _, out var emitted);

        Assert.NotNull(emitted);
        Assert.Single(emitted);
        Assert.Equal("notifications", emitted[0].StreamId);
        Assert.Equal("OrderNotification", emitted[0].EventType);
        Assert.Contains("ABC123", emitted[0].Data);
    }

    [Fact]
    public void LinkTo_produces_link_events()
    {
        using var handler = CreateHandler("""
            fromAll().when({
                OrderPlaced: function(state, event) {
                    linkTo("orders-by-customer", event);
                    return state;
                }
            })
        """);

        handler.Initialize();
        handler.ProcessEvent("", "", new ProjectionEvent
        {
            EventType = "OrderPlaced",
            StreamId = "order-1",
            Data = """{"customerId": "C1"}""",
            SequenceNumber = 42,
        }, out _, out _, out var emitted);

        Assert.NotNull(emitted);
        Assert.Single(emitted);
        Assert.Equal("orders-by-customer", emitted[0].StreamId);
        Assert.True(emitted[0].IsLink);
        Assert.Equal("42@order-1", emitted[0].Data);
    }

    [Fact]
    public void Log_calls_are_captured()
    {
        var logs = new List<string>();
        using var handler = new JintProjectionHandler("""
            fromAll().when({
                TestEvent: function(state, event) {
                    log("hello from projection");
                    return state;
                }
            })
        """, enableContentTypeValidation: false, TimeSpan.FromSeconds(5), TimeSpan.FromSeconds(5), msg => logs.Add(msg));

        handler.Initialize();
        handler.ProcessEvent("", "", new ProjectionEvent
        {
            EventType = "TestEvent",
            StreamId = "test-1",
            Data = "{}",
        }, out _, out _, out _);

        Assert.Single(logs);
        Assert.Equal("hello from projection", logs[0]);
    }

    [Fact]
    public void Any_handler_matches_all_events()
    {
        using var handler = CreateHandler("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $any: function(state, event) { state.count++; return state; }
            })
        """);

        handler.Initialize();
        handler.ProcessEvent("", "", new ProjectionEvent
        {
            EventType = "Foo",
            StreamId = "s-1",
            Data = "{}",
        }, out _, out _, out _);

        handler.ProcessEvent("", "", new ProjectionEvent
        {
            EventType = "Bar",
            StreamId = "s-1",
            Data = "{}",
        }, out var state, out _, out _);

        Assert.Contains("\"count\":2", state);
    }

    [Fact]
    public void GetSourceDefinition_returns_fromAll()
    {
        using var handler = CreateHandler("""
            fromAll().when({
                $init: function() { return {}; },
                TestEvent: function(state, event) { return state; }
            })
        """);

        var sources = handler.GetSourceDefinition();
        Assert.True(sources.AllStreams);
    }

    [Fact]
    public void GetSourceDefinition_returns_fromStream()
    {
        using var handler = CreateHandler("""
            fromStream("my-stream").when({
                $init: function() { return {}; },
                TestEvent: function(state, event) { return state; }
            })
        """);

        var sources = handler.GetSourceDefinition();
        Assert.False(sources.AllStreams);
        Assert.NotNull(sources.Streams);
        Assert.Contains("my-stream", sources.Streams);
    }

    [Fact]
    public void TransformBy_transforms_result()
    {
        using var handler = CreateHandler("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                ItemAdded: function(state, event) { state.count++; return state; }
            }).transformBy(function(state) {
                return { total: state.count };
            }).outputState()
        """);

        handler.Initialize();
        handler.ProcessEvent("", "", new ProjectionEvent
        {
            EventType = "ItemAdded",
            StreamId = "cart-1",
            Data = "{}",
        }, out _, out _, out _);

        var result = handler.TransformStateToResult();
        Assert.NotNull(result);
        Assert.Contains("\"total\":1", result);
    }
}
