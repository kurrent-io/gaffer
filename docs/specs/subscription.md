# Subscription Specification

Given a `ProjectionInfo` and a projection version, this spec defines how to subscribe to KurrentDB and feed events to the runtime.

The runtime does no I/O. Consumers build a subscription from the info, read events, and call `feed()`.

## Reference implementation

```typescript
import {
  KurrentDBClient,
  streamNameFilter,
  eventTypeFilter,
  START,
  type Filter,
} from "@kurrent/kurrentdb-client";
import type { ProjectionInfo } from "@kurrent/projections-testing";

type Version = "v1" | "v2";

/**
 * Build a $all subscription filter from projection info and version.
 */
function buildFilter(info: ProjectionInfo, version: Version): Filter | undefined {
  // Source filter: which streams/events to read
  const sourceFilter = buildSourceFilter(info, version);

  // Event type filter: layer on top for fromAll with specific events
  if (info.source.type === "all" && info.events !== "all") {
    const prefixes = [...info.events];
    if (info.settings.handlesDeletedNotifications) {
      prefixes.push("$streamDeleted", "$metadata");
    }
    return eventTypeFilter({
      prefixes,
      maxSearchWindow: 10000,
      checkpointInterval: 10,
    });
  }

  return sourceFilter;
}

function buildSourceFilter(info: ProjectionInfo, version: Version): Filter | undefined {
  switch (info.source.type) {
    case "all":
      return undefined;

    case "streams": {
      const streams = info.source.streams;

      // fromCategory multi-arg puts $ce- streams in the streams array
      if (streams.every((s) => s.startsWith("$ce-"))) {
        const categories = streams.map((s) => s.slice("$ce-".length));
        return streamNameFilter({
          prefixes: categories.map((c) => `${c}-`),
          maxSearchWindow: 10000,
          checkpointInterval: 10,
        });
      }

      return streamNameFilter({
        regex: `^(${streams.map(escapeRegex).join("|")})$`,
        maxSearchWindow: 10000,
        checkpointInterval: 10,
      });
    }

    case "categories":
      return streamNameFilter({
        prefixes: info.source.categories.map((c) => `${c}-`),
        maxSearchWindow: 10000,
        checkpointInterval: 10,
      });
  }
}

const escapeRegex = (s: string) =>
  s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

/**
 * Build a subscription from projection info.
 *
 * V1 uses resolveLinkTos: false (matching KurrentDB's TransactionFileEventReader).
 * Raw $> link events are visible to the handler when $includeLinks: true.
 *
 * V2 uses resolveLinkTos: true (matching KurrentDB's V2 read strategies).
 * Links are always resolved to their target events.
 */
function createSubscription(
  client: KurrentDBClient,
  info: ProjectionInfo,
  version: Version,
) {
  const filter = buildFilter(info, version);
  const resolveLinkTos = version === "v2";
  return client.subscribeToAll({ filter, fromPosition: START, resolveLinkTos });
}
```

## Notes

### Everything goes through $all

The reference implementation always uses `subscribeToAll` with a filter. V1's KurrentDB engine uses per-stream reads for `fromStream`/`fromCategory`, but the events are the same - just delivered in a different order for multi-stream cases. For gaffer's purposes, `$all` with a filter is simpler and correct.

A consumer MAY optimize single-stream cases with `subscribeToStream` for V1 if performance matters, but it's not required.

### Prefix matching for event types

KurrentDB V2 uses prefix matching at the read layer. `eventTypeFilter({ prefixes: ["Order"] })` matches `Order`, `OrderPlaced`, `OrderShipped`. The runtime does exact dispatch internally via `ShouldProcess`. Over-subscribing is safe, under-subscribing means missing events.

### `events: "all"` vs `events: [...]` is a tagged union

`info.events` is `"all"` when the projection's `when()` block contains a `$any` handler, and an array of specific event-type names otherwise. The check at line 29 (`info.events !== "all"`) is what gates the event-type filter on - when it's `"all"`, every event type is handled and **no filter applies**.

If a binding flattens this union into two separate fields (e.g. a Go `AllEvents bool` plus `Events []string`), watch out: the runtime populates the names array with the specific handler names *even when `$any` is also present*. So `Events` being non-empty does not imply "filter by these". The `AllEvents` flag is the source of truth, and any check on the array's length must be gated on `!AllEvents` first.

This caught us once (gaffer's Go CLI silently filtered out every event-type that should have hit `$any`). Tests below cover the regression.

### Subscription read parameters

KurrentDB clients default `MaxSearchWindow` to 32 and `CheckpointInterval` to 1 for filtered subscriptions. With a narrow filter on a large `$all` (anything beyond a fresh local instance), the server checkpoints after every 32 events read, producing thousands of round-trips before the read pointer reaches the live tail. CaughtUp can take minutes or never fire within a usable timeout.

Recommend `maxSearchWindow: 10000`, `checkpointInterval: 10` or similar. Verified against a 6 GB managed instance: catch-up under defaults effectively never fires; under 10000/10 it fires in ~40 seconds.

Where these go is client-shaped: TypeScript puts them on the filter object (`eventTypeFilter` / `streamNameFilter` arguments). Go puts them on the top-level subscribe options. Either way, an unfiltered subscription (`return undefined` from `buildFilter`) doesn't need them - the parameters only matter when the server is scanning past skipped events.

### V1 fromCategory needs $by_category

When version is `v1` and source is `categories`, the `$ce-{category}` streams must exist. These are populated by the `$by_category` system projection. If it's not enabled, the filter approach still works (matches `order-*` streams in `$all`) but event ordering may differ from production V1.

### System events

The runtime detects and routes `$streamDeleted` and soft-delete `$metadata` events internally. When `handlesDeletedNotifications` is true, these event types are added to the prefix filter so they pass through.

### Link events and $includeLinks

The runtime handles `$includeLinks` filtering internally. When `$includeLinks` is false (default), the runtime drops raw link events (`$>` event type) and resolved-from-link events (events with `linkMetadata` set) in `Feed()`.

V1 subscribes with `resolveLinkTos: false`, so raw `$>` events appear in the stream. When `$includeLinks: true`, these reach the handler as events with `eventType: "$>"` and `bodyRaw: "0@source-stream"`. When `$includeLinks: false`, the runtime drops them.

V2 subscribes with `resolveLinkTos: true`, so links are always resolved to their target events before reaching the consumer. The `$includeLinks` option controls whether resolved-from-link events (identified by `linkMetadata` being present) are passed through or dropped.

### reorderEvents / processingLag

V1 only. Only valid for `fromStreams` with more than one stream. `processingLag` must be at least 50ms. V2 does not support it.

This option exists because V1 merges separate per-stream reads, which can deliver events out of global order. Since this spec always uses `subscribeToAll` with a filter, events arrive in commit position order naturally. `reorderEvents` and `processingLag` are effectively noops for the `$all` approach - consumers can safely ignore them.

### data must be null, not "null"

When an event has no body, `data` must be `null`, not the 4-character string `"null"`. The runtime treats these differently.

### fromCategory multi-arg edge case

`fromCategory("order")` produces `source.type = "categories"` with `categories: ["order"]`.
`fromCategory(["order", "cart"])` produces `source.type = "streams"` with `streams: ["$ce-order", "$ce-cart"]`.

The reference implementation detects `$ce-` prefixed streams and converts them to category prefix filters.

## Test cases for `buildFilter`

A faithful implementation should cover at least the following. Each case is a one-liner: input shape → expected filter.

**Source filtering**

- `fromAll()` with no handlers and no `$any`: no filter.
- `fromAll()` with specific event handlers, no `$any`: event-type prefix filter with the handler names.
- `fromAll()` with `$any` (alone, or alongside specific handlers): **no filter** (every event type is handled). Regression case for the `events: "all"` union.
- `fromAll()` with specific events plus a `$deleted` handler: event-type prefix filter with the handler names plus `$streamDeleted` and `$metadata`.
- `fromCategory("order")`: stream-name prefix filter `["order-"]`.
- `fromCategory(["order", "cart"])`: stream-name prefix filter `["order-", "cart-"]` (note: input arrives as `["$ce-order", "$ce-cart"]` and must be detected and rewritten).
- `fromStream("order-1")` / `fromStreams("order-1", "cart-1")`: stream-name regex anchored on the exact names.

**Link resolution**

- Engine version 1: `resolveLinkTos: false`.
- Engine version 2: `resolveLinkTos: true`.

**Read parameters**

- Filtered subscription: `MaxSearchWindow` and `CheckpointInterval` set to non-default values (32/1 is too small for production stores). Exact values are an implementation choice; the test should assert they were set, not that they match a specific number.
