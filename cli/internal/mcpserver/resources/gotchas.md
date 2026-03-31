# Projection Gotchas

Common mistakes, constraints, and surprising behavior when writing projections.

## Always return state from handlers

The most common mistake. If you forget to return from a handler, state becomes
`undefined` on the next handler call and your projection silently breaks.

```javascript
// WRONG - state becomes undefined
OrderPlaced: function(s, e) {
    s.count += 1;
}

// CORRECT - always return state
OrderPlaced: function(s, e) {
    s.count += 1;
    return s;
}

// CORRECT - explicit reset with null
OrderCancelled: function(s, e) {
    return null;
}
```

This applies to all handlers except `$init` (which returns the initial state),
`$created` (return value discarded), and `$deleted` (return value discarded,
mutate in-place).

## $any must be the last handler

If `$any` appears before named handlers in the `when({})` object, the runtime
marks the projection as not handling all events. Named event types still work,
but events that don't match a named handler will never reach `$any` - they are
silently dropped.

```javascript
// WRONG - $any will miss unmatched events
.when({
    $init: function() { return { count: 0 }; },
    $any: function(s, e) { s.count += 1; return s; },
    OrderPlaced: function(s, e) { s.count += 10; return s; }
})

// CORRECT - $any listed last
.when({
    $init: function() { return { count: 0 }; },
    OrderPlaced: function(s, e) { s.count += 10; return s; },
    $any: function(s, e) { s.count += 1; return s; }
})
```

## Execution timeout

Each handler invocation has a 250ms default execution timeout. If your handler
takes longer, the projection faults. This is configurable per-projection but
keep handlers fast - do simple state mutations, not heavy computation.

## JSON only

Event bodies must be JSON. Non-JSON event data is not accessible in handlers -
`body` will be null and `isJson` will be false. The event still passes through
the projection (it isn't skipped), but you can only access metadata and stream
information.

## 16MB state limit

Projection state has a hard size limit of 16MB (warning at 8MB). If your
projection accumulates unbounded state, it will eventually fail.

Aggregate and summarize rather than storing raw event payloads in state. If you
need to track large datasets, emit events to streams instead of keeping them
in state.

```javascript
// WRONG - state grows without bound
$any: function(s, e) {
    s.events.push(e.body);
    return s;
}

// BETTER - emit to a stream, keep only a summary in state
OrderPlaced: function(s, e) {
    s.orderCount += 1;
    s.totalCents += e.body.cents;
    emit('order-log', 'OrderTracked', { orderId: e.streamId });
    return s;
}
```

## Stream ownership

Streams that receive projection output (via `emit`, `linkTo`, or `linkStreamTo`)
are exclusively owned by the projection. If application code writes to a stream
that a projection also writes to, the projection will break. This includes
`$`-prefixed system streams.

Plan your stream naming so projection output streams don't collide with
application streams.

## Write amplification

Every `emit()` and `linkTo()` call is a write operation. A `fromAll()` projection
that emits for every event doubles your write load. System projections compound
this further - if `$by_category`, `$by_event_type`, and `$by_correlation_id` are
all enabled, each original event produces 3 additional link events.

Custom projection emits are also processed by system projections, further
multiplying writes. Be deliberate about what you emit.

## partitionBy edge cases

The partition function can return surprising values:

- `NaN` or `Infinity` create partition keys `"NaN"` or `"Infinity"` - technically
  valid but almost certainly not intended
- Empty string `""` creates a partition with an empty key
- `null` or `undefined` skip the event (it won't be processed)

Always validate or constrain the value you return from `partitionBy`.

## $deleted constraints

The `$deleted` handler has several restrictions:

- Only works with `foreachStream` - not with `partitionBy`
- Not available in bi-state mode
- The `event` parameter is always `null` (not an event object)
- The return value is discarded - you must mutate state in-place
- `isSoftDelete` is always `false`

```javascript
// CORRECT - mutate in-place, don't rely on return
$deleted: function(s, event, partition, isSoftDelete) {
    s.active = false;
    s.deletedAt = new Date().toISOString();
}
```

## Unhandled events overwrite state

When an event matches no named handler and no `$any` is defined, the runtime
replaces state with the raw event body string. Your state object silently becomes
a JSON string and subsequent handlers will fail or behave unexpectedly.

Either handle all expected event types with named handlers, or add `$any` as a
catch-all. If you want to ignore unmatched events, return state unchanged:

```javascript
$any: function(s, e) {
    return s;
}
```

## Prefer narrower sources over fromAll

`fromAll()` processes every event in the database. For large event stores this
is expensive and slow. Prefer `fromCategory` or `fromStream` to narrow the
subscription scope. If you need events from multiple categories, use
`fromCategories` rather than `fromAll` with filtering in handlers.

## $created is side-effect only

The `$created` handler fires when a new partition is first seen, but its return
value is discarded. Use it for logging or emitting, not for setting initial state
(that's what `$init` is for).
