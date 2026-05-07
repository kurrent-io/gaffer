# Projection API Reference

KurrentDB projection API as executed by gaffer. Documents V2 behavior (gaffer default).
For V1 differences, see the v1-v2-differences resource.

Projections are JavaScript programs that process event streams and produce derived
state, new events, or event links. They run inside a sandboxed JS engine (Jint)
with a specific set of global functions and a fluent API for declaring what to process
and how.

## Source functions

Source functions declare which events the projection reads. Call one at the top
level of your projection script. Each returns a chain object for further configuration.

### fromAll()

Read every event from the `$all` stream.

```javascript
fromAll()
```

Returns a chain with: `when`, `foreachStream`, `partitionBy`, `outputState`.

Use when you need to process events across all streams. Be aware this processes
every event in the database - for large stores, prefer a narrower source if possible.

### fromStream(streamId)

Read from a single named stream.

```javascript
fromStream('orders-123')
```

Returns a chain with: `when`, `partitionBy`, `outputState`.

`foreachStream` is not available since there is only one stream.

### fromStreams(streamId, ...)

Read from multiple named streams. Accepts variadic arguments or an array.

```javascript
fromStreams('orders-123', 'orders-456')
fromStreams(['orders-123', 'orders-456'])
```

Returns a chain with: `when`, `partitionBy`, `outputState`.

### fromCategory(category)

Read all streams in a category. A category is the stream ID prefix before the
first `-` separator. For example, streams `order-1`, `order-2`, `order-3` all
belong to the `order` category.

```javascript
fromCategory('order')
```

Returns a chain with: `when`, `foreachStream`, `partitionBy`, `outputState`.

### fromCategories(category, ...)

Read from multiple categories. Accepts variadic arguments or an array.

```javascript
fromCategories('order', 'invoice')
fromCategories(['order', 'invoice'])
```

Returns the same chain as `fromCategory`.

Note: `fromCategory` also accepts multiple arguments and arrays at the runtime
level, but prefer `fromCategories` when reading from multiple categories for
clarity.

## Chain methods

Chain methods are called on the object returned by a source function. They configure
how events are processed, partitioned, and output.

### .when(handlers)

Register event handlers. This is where your projection logic lives.

```javascript
fromCategory('order')
.foreachStream()
.when({
    $init() {
        return { total: 0 };
    },
    OrderPlaced(s, e) {
        s.total += e.body.cents;
        return s;
    }
})
```

`handlers` is an object where keys are event type names (or special `$` handlers)
and values are handler functions. See the Handlers section below for all signatures.

Returns a chain with: `transformBy`, `filterBy`, `outputTo`, `outputState`.

### .foreachStream()

Partition state by source stream. Each stream gets its own independent state
instance. `$init` runs once per stream the first time an event from that stream
is seen.

```javascript
fromCategory('order')
.foreachStream()
.when({ ... })
```

Only available after `fromAll()` or `fromCategory()`. Returns a chain with only `when`.

### .partitionBy(fn)

Partition state by a custom key derived from each event. The function receives
the event and returns a partition key.

```javascript
fromAll()
.partitionBy(function(e) {
    return e.body.customerId;
})
.when({ ... })
```

`fn` signature: `(event) => string | number | null | undefined`

- Returning a string or number assigns the event to that partition
- Returning `null` or `undefined` skips the event (it won't be processed)

Returns a chain with only `when`.

### .outputState()

**V1:** Write projection state to the result stream `$projections-{name}-result`
after each event is processed. Without this, state is only held in memory.

**V2:** No effect - state is always written to the result stream regardless.
Calls under `engine_version=2` emit `compat.outputState.unconditional` (Hint).

```javascript
fromCategory('order')
.foreachStream()
.when({ ... })
.outputState()
```

Returns a chain with: `transformBy`, `filterBy`, `outputTo`.

### .transformBy(fn)

**V1:** Transform the state before it is output. Does not affect the state seen
by handlers - only the output representation. Can be chained multiple times.

**V2:** Not invoked. The function is registered at definition time but never
runs on events; the result stream receives post-handler state. Calls under
`engine_version=2` emit `compat.transforms.notApplied` (Warning).

```javascript
.when({ ... })
.transformBy(function(s) {
    return { averageCents: s.count > 0 ? s.totalCents / s.count : 0 };
})
```

`fn` signature: `(state) => newState`

The return type becomes the new output state type. Returns a chain with:
`transformBy`, `filterBy`, `outputTo`, `outputState`.

### .filterBy(fn)

**V1:** Filter output by a predicate. When the predicate returns false, the
result is suppressed (null is output instead).

**V2:** Not invoked. The function is registered at definition time but never
runs on events; the result stream receives post-handler state. Calls under
`engine_version=2` emit `compat.transforms.notApplied` (Warning).

```javascript
.when({ ... })
.filterBy(function(s) {
    return s.total > 1000;
})
```

`fn` signature: `(state) => boolean`

Returns a chain with: `transformBy`, `filterBy`, `outputTo`, `outputState`.

### .outputTo(streamName, partitionPattern?)

Write output to a named stream instead of the default result stream.
The optional partition pattern uses `{0}` as a placeholder for the partition key.

```javascript
.when({ ... })
.outputTo('order-summaries', 'order-summary-{0}')
```

This is a terminal method - no further chaining.

## options(config)

Set projection-level configuration. Call at the top level, before or after the
source function.

```javascript
options({
    resultStreamName: 'my-custom-result-stream'
})
```

### Available options

| Option | Type | Description |
|---|---|---|
| `resultStreamName` | string | Override the default result stream name |
| `biState` | boolean | Enable bi-state mode (see BiState section). Defaults to false. Automatically enabled when `$initShared` is present in handlers |

Additional V1-only options (`$includeLinks`, `reorderEvents`, `processingLag`) are
documented in the v1-v2-differences resource.

## Handlers

Handlers are the functions inside `when({...})` that process events. Each handler
receives state and an event, and must return the new state.

### $init

```javascript
$init() {
    return { count: 0, totalCents: 0 };
}
```

Called once per partition (or once globally if unpartitioned) to create the initial
state. Returns the initial state object. Does not receive any arguments.

If `$init` is omitted, the runtime uses an empty object `{}` as the default
initial state. Always define `$init` explicitly for clarity.

### Named event handlers

```javascript
OrderPlaced(s, e) {
    s.count += 1;
    s.totalCents += e.body.cents;
    return s;
}
```

Called when an event with a matching `eventType` is processed. The key must exactly
match the event type string.

Signature: `(state, event) => state | null | void`

Always return the state. Omitting the return makes state `undefined` on the next
handler call. Return `null` to explicitly reset state.

### $any

```javascript
$any(s, e) {
    s.eventCount += 1;
    return s;
}
```

Catch-all handler for events not matched by a named handler. If a named handler
matches the event type, `$any` is not called for that event.

IMPORTANT: `$any` must be the last handler listed in the `when({})` object. If
listed before named handlers, the runtime restricts the event subscription to
only the named types, and unmatched events will never reach `$any`.

Signature: `(state, event) => state | null | void`

### $created

```javascript
$created(s, e) {
    log(`New partition: ${e.partition}`);
}
```

Called when a new partition is first seen. Only fires with `foreachStream` or
`partitionBy`. The return value is discarded - this handler is for side effects
only (logging, emitting).

Signature: `(state, event) => void`

### $deleted

```javascript
$deleted(s, event, partition, isSoftDelete) {
    s.active = false;
}
```

Called when a stream is deleted. Only works with `foreachStream` - not available
with `partitionBy` or in bi-state mode.

- `event` is always `null`
- `partition` is the stream ID being deleted (string)
- `isSoftDelete` is always `false`
- The return value is discarded - mutate state in-place

Signature: `(state, null, partition, isSoftDelete) => void`

## Side-effect functions

These global functions are available inside handlers for emitting events and logging.

### emit(streamId, eventType, eventBody, metadata?)

Append a new event to a stream.

```javascript
emit('order-notifications', 'OrderAlert', {
    orderId: e.streamId,
    message: 'New order placed'
}, { source: 'order-projection' });
```

- `streamId` - target stream (string)
- `eventType` - event type name (string)
- `eventBody` - event data (object, will be JSON serialized)
- `metadata` - optional metadata object

The target stream becomes exclusively owned by this projection - application code
cannot write to it.

### linkTo(streamId, event, metadata?)

Create a link event pointing to the source event in another stream. No data
duplication - just a pointer to the original event.

```javascript
linkTo('high-value-orders', e, e.metadata);
```

- `streamId` - target stream (string)
- `event` - the full event object (not a string or ID)
- `metadata` - optional metadata object

### linkStreamTo (deprecated, buggy)

Emits a `$@` event referencing an entire stream rather than a single event.
**Avoid in new projections.** Undocumented in KurrentDB and may be removed
in a future version. The 3-argument metadata form crashes at runtime due
to an upstream bug (see the `db-version-bugs` MCP resource for details).

For single-event references prefer `linkTo`. There is no clean replacement
for stream-level references; if you need one, document the constraint and
guard the projection accordingly.

### log(message)

Log a single message string. Output goes to the projection log, visible
in gaffer's output.

```javascript
log(`Processing order: ${e.streamId} total: ${e.body.cents}`);
```

**Use one argument only.** Calling `log` with multiple arguments triggers
an upstream bug that splits primitives across separate log lines and
joins objects with a `' ,'` separator (see `db-version-bugs` resource).
Build the string yourself with a template literal or concatenation.

## Event envelope

The `event` parameter (commonly named `e`) passed to handlers is a `KurrentEvent`
object with the following properties:

| Property | Type | Description |
|---|---|---|
| `streamId` | string | Source stream the event belongs to |
| `eventType` | string | Event type name |
| `body` | object or null | Parsed JSON event body. Null when the event body is not JSON. This is the primary property for accessing event data |
| `bodyRaw` | string or null | Raw JSON string of the body. Null when zero-length |
| `metadata` | object or null | Parsed metadata object |
| `metadataRaw` | string or null | Raw metadata JSON string |
| `linkMetadata` | object or null | Parsed metadata from the link event, when processing a resolved link |
| `linkMetadataRaw` | string or null | Raw link metadata string |
| `sequenceNumber` | number | Event number within its stream (stream revision) |
| `eventId` | string | Unique event identifier |
| `isJson` | boolean | True when the event body is JSON |
| `created` | string | ISO 8601 datetime of event creation |
| `category` | string | Stream category extracted from streamId (prefix before first `-`) |
| `partition` | string | Current partition key. Empty string when unpartitioned |

`data` is a deprecated alias for `body`. You may encounter it in older projections
but `body` is the recommended property.

## BiState mode

BiState allows a projection to maintain both per-stream state and a single shared
state across all streams. It requires `foreachStream` - biState is not available
with `partitionBy` or unpartitioned projections.

When biState is active, handlers receive the state as a `[partitionState, sharedState]`
array and must return the same structure.

```javascript
fromAll()
.foreachStream()
.when({
    $init() {
        return { streamCount: 0 };
    },
    $initShared() {
        return { globalCount: 0 };
    },
    $any(state, e) {
        var s = state[0];
        var shared = state[1];
        s.streamCount += 1;
        shared.globalCount += 1;
        return [s, shared];
    }
})
```

BiState is automatically enabled when `$initShared` is present in handlers, or
explicitly via `options({ biState: true })`.

`$initShared` is required - it initializes the shared state. `$init` initializes
the per-partition state as normal.

`$deleted` is not available in bi-state mode.

## Alternative handler registration

These global functions are an alternative to the `when()` chain pattern. You may
encounter them in existing projections.

### on_event(eventName, handler)

Register a handler for a specific event type.

```javascript
on_event('OrderPlaced', function(s, e) {
    s.count += 1;
    return s;
});
```

### on_any(handler)

Register a catch-all handler.

```javascript
on_any(function(s, e) {
    s.count += 1;
    return s;
});
```

## Chain availability

What methods are available at each step of the fluent API:

| After | Available |
|---|---|
| `fromAll()` | `when`, `foreachStream`, `partitionBy`, `outputState` |
| `fromCategory()` | `when`, `foreachStream`, `partitionBy`, `outputState` |
| `fromStream()` | `when`, `partitionBy`, `outputState` |
| `fromStreams()` | `when`, `partitionBy`, `outputState` |
| `.foreachStream()` | `when` |
| `.partitionBy()` | `when` |
| `.when()` | `transformBy`, `filterBy`, `outputTo`, `outputState` |
| `.outputState()` | `transformBy`, `filterBy`, `outputTo` |
| `.transformBy()` | `transformBy`, `filterBy`, `outputTo`, `outputState` |
| `.filterBy()` | `transformBy`, `filterBy`, `outputTo`, `outputState` |
| `.outputTo()` | (terminal) |
