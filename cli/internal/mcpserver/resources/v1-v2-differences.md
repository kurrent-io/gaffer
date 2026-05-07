# V1 vs V2 Projection Differences

Gaffer defaults to V2. Read this when working with V1 projections or migrating
from V1 to V2. These are silent behavioral differences where code that looks
correct behaves differently between versions.

## Non-JSON events

**V1:** Non-JSON events are dropped at the subscription layer before they reach
your projection. Your handlers never see them.

**V2:** All events pass through. Non-JSON events arrive with `body: null` and
`isJson: false`. Your handlers run but can only access metadata and stream info.

**Impact:** A V1 projection that assumes every event has a body will work fine.
The same projection on V2 may fail if it accesses `e.body.someField` on a non-JSON
event without checking `e.isJson` first.

## Null state behavior

**V1:** If a handler returns `null`, the state is coerced to an empty string `""`.
On the next visit to that partition, `$init` runs again as if the partition
were new.

**V2:** Null state is preserved. `$init` does not re-run. The next handler call
receives `null` as state.

**Impact:** V1 projections that return `null` to "reset" a partition rely on
`$init` re-running. On V2 this won't happen - the state stays null. If you want
to reset state on V2, return a fresh state object from the handler instead.

## Link event handling

**V1:** The `$includeLinks` option controls whether link events are processed.
Defaults to `false` - link events are filtered out.

**V2:** `$includeLinks` is parsed but ignored. Link events are always resolved
to their target events.

**Impact:** V1 projections that set `$includeLinks: true` to process raw link
events will behave differently on V2, where link events are always resolved
regardless of this setting.

## V1-only options

These options are functional on V1 but parsed and ignored on V2:

### $includeLinks

```javascript
options({ $includeLinks: true })
```

When true, link events are passed to handlers as-is rather than being filtered.
On V2, links are always resolved - this option has no effect.

### reorderEvents

```javascript
options({ reorderEvents: true, processingLag: 500 })
```

Buffer events and reorder them by timestamp. Only works with `fromStreams`.
Requires `processingLag` to be set to at least 50ms.

### processingLag

```javascript
options({ processingLag: 500 })
```

Delay in milliseconds before processing events. Minimum 50ms. Used with
`reorderEvents` to allow time for out-of-order events to arrive before processing.

## System event filtering

**V1:** Some `$`-prefixed system events reach your handlers. A `$any` handler
may see system events mixed in with application events.

**V2:** All `$`-prefixed events (except stream deletion events) are dropped before
reaching handlers. Your handlers only see application events and deletion
notifications.

**Impact:** V1 projections with `$any` that happen to process system events
(intentionally or not) will miss those events on V2.

## Event type matching

**V1:** Exact match on event type names in `when()` handlers. An event with type
`OrderPlaced` only matches the `OrderPlaced` handler.

**V2:** The read layer uses prefix matching on event types, but the JavaScript
handler dispatch still does exact matching. Events matched by prefix but not
exactly matching a handler name will reach `$any` (if defined) but won't match
a named handler.

**Impact:** Rarely noticeable unless you have event types that are prefixes of
other event types (e.g., `Order` and `OrderPlaced`).

## Subscription strategy

**V1 `fromCategory`:** Reads from `$ce-{category}` streams. Requires the
`$by_category` system projection to be enabled and running.

**V2 `fromCategory`:** Filters the `$all` stream by stream name prefix. Does
not depend on system projections.

**V1 `fromStream`:** Reads the specific stream directly.

**V2 `fromStream`:** Reads `$all` with a stream name filter.

**Impact:** V1 projections using `fromCategory` require `$by_category` to be
enabled. V2 projections don't have this dependency. When migrating from V1 to V2,
system projection dependencies can be removed.

## Transforms

**V1:** `transformBy(fn)` and `filterBy(fn)` run after each handler. The result
emitted to the result stream is the transformed/filtered value, not the raw
state. `outputState()` opts the projection in to result-stream emission.

**V2:** Transforms are never invoked. The post-handler state is written
directly to the result stream by `PartitionProcessor`; there is no transform
pipeline. `transformBy` / `filterBy` / `outputState` are still callable at
definition time (the JS calls succeed silently), but the functions passed to
them never run on events. State emission to the result stream is automatic
and unconditional - `outputState()` has no effect.

**Impact:** A V2 projection that calls `transformBy` to derive a result will
get the post-handler state back instead. Likewise for `filterBy` exclusion
predicates. `outputState()` is redundant - state is always emitted.

Gaffer matches this behaviour by design and surfaces the gap as compile-time
diagnostics so you find it before the result stream surprises you:

* `compat.transforms.notApplied` (Warning) on `transformBy` / `filterBy`
  calls when `engine_version=2`.
* `compat.outputState.implicit` (Hint) on `outputState()` calls when
  `engine_version=2`.

<!-- TODO: cite engine-v2.md once it lands on KurrentDB master. -->

