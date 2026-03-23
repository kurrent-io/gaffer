using Gaffer.Runtime.Events;

namespace Gaffer.Runtime.Tests;

public class EndToEndTests
{
    private const string OrderProjection = """
        fromCategory("order")
        .foreachStream()
        .when({
            $init: function() {
                return { count: 0, totalAmount: 0 };
            },
            OrderPlaced: function(s, e) {
                s.count++;
                s.totalAmount += e.data.amount;
                emit("order-summary-" + e.streamId, "OrderSummaryUpdated", {
                    stream: e.streamId,
                    count: s.count,
                    totalAmount: s.totalAmount
                });
                return s;
            },
            OrderShipped: function(s, e) {
                s.count++;
                return s;
            }
        })
        """;

    [Fact]
    public void Category_partitioned_state_with_emit()
    {
        var emitted = new List<EmittedEvent>();
        using var session = new ProjectionSession(OrderProjection);
        session.OnEmit = e => emitted.Add(e);

        // Order stream 1: OrderPlaced, OrderShipped, OrderPlaced
        session.Feed(new ProjectionEvent { EventType = "OrderPlaced", StreamId = "order-1", Data = """{"amount":100.50,"item":"Widget"}""" });
        session.Feed(new ProjectionEvent { EventType = "OrderShipped", StreamId = "order-1", Data = """{"trackingId":"TRK-001"}""" });
        session.Feed(new ProjectionEvent { EventType = "OrderPlaced", StreamId = "order-1", Data = """{"amount":200.00,"item":"Gadget"}""" });

        // Order stream 2: OrderPlaced
        session.Feed(new ProjectionEvent { EventType = "OrderPlaced", StreamId = "order-2", Data = """{"amount":50.00,"item":"Doohickey"}""" });

        // Non-matching event type in matching stream (counted but no emit)
        session.Feed(new ProjectionEvent { EventType = "OrderNoteAdded", StreamId = "order-1", Data = """{"note":"Please gift wrap"}""" });

        // Verify order-1 state: 3 counted events (2 OrderPlaced + 1 OrderShipped), totalAmount from placed only
        var order1 = session.GetState("order-1")!;
        Assert.Contains("\"count\":3", order1);
        Assert.Contains("\"totalAmount\":300.5", order1);

        // Verify order-2 state
        var order2 = session.GetState("order-2")!;
        Assert.Contains("\"count\":1", order2);
        Assert.Contains("\"totalAmount\":50", order2);

        // Verify emitted summary events (only from OrderPlaced, not OrderShipped or OrderNoteAdded)
        var summaries = emitted.Where(e => e.EventType == "OrderSummaryUpdated").ToList();
        Assert.Equal(3, summaries.Count);

        // First emit for order-1
        Assert.Equal("order-summary-order-1", summaries[0].StreamId);
        Assert.Contains("\"count\":1", summaries[0].Data);
        Assert.Contains("\"totalAmount\":100.5", summaries[0].Data);

        // Second emit for order-1
        Assert.Equal("order-summary-order-1", summaries[1].StreamId);
        Assert.Contains("\"count\":3", summaries[1].Data);
        Assert.Contains("\"totalAmount\":300.5", summaries[1].Data);

        // Emit for order-2
        Assert.Equal("order-summary-order-2", summaries[2].StreamId);
        Assert.Contains("\"count\":1", summaries[2].Data);
        Assert.Contains("\"totalAmount\":50", summaries[2].Data);
    }

    [Fact]
    public void Non_matching_category_events_ignored()
    {
        using var session = new ProjectionSession(OrderProjection);

        // These are "invoice-" category, not "order-"
        session.Feed(new ProjectionEvent { EventType = "InvoiceCreated", StreamId = "invoice-1", Data = """{"total":999}""" });

        // invoice-1 should not have state (different category, foreachStream partitions by stream)
        // The event won't be filtered by ShouldProcess since OrderPlaced/OrderShipped are specific handlers
        // But the partition will be "invoice-1" which is fine - it just won't match any handlers
        Assert.Null(session.GetState("invoice-1"));
    }

    [Fact]
    public void Interleaved_events_across_streams()
    {
        var emitted = new List<EmittedEvent>();
        using var session = new ProjectionSession(OrderProjection);
        session.OnEmit = e => emitted.Add(e);

        // Interleave events across multiple streams
        session.Feed(new ProjectionEvent { EventType = "OrderPlaced", StreamId = "order-a", Data = """{"amount":10}""" });
        session.Feed(new ProjectionEvent { EventType = "OrderPlaced", StreamId = "order-b", Data = """{"amount":20}""" });
        session.Feed(new ProjectionEvent { EventType = "OrderPlaced", StreamId = "order-a", Data = """{"amount":30}""" });
        session.Feed(new ProjectionEvent { EventType = "OrderShipped", StreamId = "order-b", Data = "{}" });
        session.Feed(new ProjectionEvent { EventType = "OrderPlaced", StreamId = "order-c", Data = """{"amount":40}""" });
        session.Feed(new ProjectionEvent { EventType = "OrderPlaced", StreamId = "order-a", Data = """{"amount":50}""" });

        Assert.Contains("\"count\":3", session.GetState("order-a")!);
        Assert.Contains("\"totalAmount\":90", session.GetState("order-a")!);

        Assert.Contains("\"count\":2", session.GetState("order-b")!);
        Assert.Contains("\"totalAmount\":20", session.GetState("order-b")!);

        Assert.Contains("\"count\":1", session.GetState("order-c")!);
        Assert.Contains("\"totalAmount\":40", session.GetState("order-c")!);

        // 5 OrderPlaced events = 5 emits
        Assert.Equal(5, emitted.Count(e => e.EventType == "OrderSummaryUpdated"));
    }
}
