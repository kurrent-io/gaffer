# Projection Examples

Working projection patterns that run in gaffer. All examples use an order
management domain for consistency.

Event types used across examples: `OrderPlaced`, `OrderShipped`, `OrderDelivered`,
`OrderCancelled`, `ItemAdded`, `PaymentReceived`.

## Simple counter

Count all orders placed across the entire event store.

```javascript
fromAll()
.when({
    $init() {
        return { count: 0, totalCents: 0 };
    },
    OrderPlaced(s, e) {
        s.count += 1;
        s.totalCents += e.body.cents;
        return s;
    }
})
```

## Per-stream aggregation

Track order totals per customer using `foreachStream`. Each stream in the `order`
category gets its own independent state.

```javascript
fromCategory('order')
.foreachStream()
.when({
    $init() {
        return { items: 0, totalCents: 0, status: 'open' };
    },
    ItemAdded(s, e) {
        s.items += 1;
        s.totalCents += e.body.priceCents;
        return s;
    },
    OrderShipped(s, e) {
        s.status = 'shipped';
        return s;
    },
    OrderDelivered(s, e) {
        s.status = 'delivered';
        return s;
    }
})
```

## Custom partitioning

Group orders by customer, regardless of which stream they belong to. The partition
key is extracted from the event body.

```javascript
fromCategory('order')
.partitionBy(function(e) {
    return e.body.customerId;
})
.when({
    $init() {
        return { orderCount: 0, totalSpentCents: 0 };
    },
    OrderPlaced(s, e) {
        s.orderCount += 1;
        s.totalSpentCents += e.body.cents;
        return s;
    }
})
```

## BiState

Track both per-stream order state and a global summary using bi-state mode.
Handlers receive `[partitionState, sharedState]` and must return the same structure.

```javascript
fromCategory('order')
.foreachStream()
.when({
    $init() {
        return { totalCents: 0 };
    },
    $initShared() {
        return { orderCount: 0, revenueCents: 0 };
    },
    OrderPlaced(state, e) {
        var s = state[0];
        var shared = state[1];
        s.totalCents += e.body.cents;
        shared.orderCount += 1;
        shared.revenueCents += e.body.cents;
        return [s, shared];
    }
})
```

## Emit pattern

Process orders and emit notification events to a separate stream. Use state to
track what has already been emitted to avoid duplicates on replay.

```javascript
fromCategory('order')
.foreachStream()
.when({
    $init() {
        return { notified: false };
    },
    OrderPlaced(s, e) {
        if (!s.notified) {
            emit('order-notifications', 'NewOrderAlert', {
                orderId: e.streamId,
                cents: e.body.cents,
                customer: e.body.customerId
            });
            s.notified = true;
        }
        return s;
    }
})
```

## LinkTo pattern

Build a curated stream of high-value orders by linking events. No data duplication -
each link is a pointer to the original event.

```javascript
fromCategory('order')
.when({
    $init() {
        return {};
    },
    OrderPlaced(s, e) {
        if (e.body.cents >= 10000) {
            linkTo('high-value-orders', e, e.metadata);
        }
        return s;
    }
})
```

Dynamic stream routing - group orders by region based on event data:

```javascript
fromCategory('order')
.when({
    $init() {
        return {};
    },
    OrderPlaced(s, e) {
        var region = e.body.region;
        linkTo('orders-' + region, e, e.metadata);
        return s;
    }
})
```

## Stream deletion

Handle stream deletion in a per-stream projection. The `$deleted` handler mutates
state in-place (return value is discarded). The `event` parameter is null.

```javascript
fromCategory('order')
.foreachStream()
.when({
    $init() {
        return { active: true, totalCents: 0 };
    },
    OrderPlaced(s, e) {
        s.totalCents += e.body.cents;
        return s;
    },
    $deleted(s, event, partition, isSoftDelete) {
        s.active = false;
    }
})
```

## Transform and filter

Compute a derived view from raw state using `transformBy`, then suppress results
below a threshold with `filterBy`. Transforms don't affect handler state - only
the output.

```javascript
fromCategory('order')
.foreachStream()
.when({
    $init() {
        return { count: 0, totalCents: 0 };
    },
    ItemAdded(s, e) {
        s.count += 1;
        s.totalCents += e.body.priceCents;
        return s;
    }
})
.transformBy(function(s) {
    return {
        itemCount: s.count,
        averagePriceCents: s.count > 0 ? Math.round(s.totalCents / s.count) : 0
    };
})
.filterBy(function(s) {
    return s.itemCount > 0;
})
.outputState()
```

## Catching all events

Use `$any` to handle events that don't match a named handler. Remember: `$any`
must be the last handler listed.

```javascript
fromCategory('order')
.foreachStream()
.when({
    $init() {
        return { eventCount: 0, lastEventType: null };
    },
    OrderCancelled(s, e) {
        log(`Order cancelled: ${e.streamId}`);
        emit('cancellations', 'OrderCancelled', { orderId: e.streamId });
        s.eventCount += 1;
        s.lastEventType = e.eventType;
        return s;
    },
    $any(s, e) {
        s.eventCount += 1;
        s.lastEventType = e.eventType;
        return s;
    }
})
```
