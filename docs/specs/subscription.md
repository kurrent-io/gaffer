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
    return eventTypeFilter({ prefixes });
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
        return streamNameFilter({ prefixes: categories.map((c) => `${c}-`) });
      }

      return streamNameFilter({
        regex: `^(${streams.map(escapeRegex).join("|")})$`,
      });
    }

    case "categories":
      return streamNameFilter({
        prefixes: info.source.categories.map((c) => `${c}-`),
      });
  }
}

const escapeRegex = (s: string) =>
  s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

/**
 * Build a subscription from projection info.
 *
 * V1 uses resolveLinks: false (matching KurrentDB's TransactionFileEventReader).
 * Raw $> link events are visible to the handler when $includeLinks: true.
 *
 * V2 uses resolveLinks: true (matching KurrentDB's V2 read strategies).
 * Links are always resolved to their target events.
 */
function createSubscription(
  client: KurrentDBClient,
  info: ProjectionInfo,
  version: Version,
) {
  const filter = buildFilter(info, version);
  const resolveLinks = version === "v2";
  return client.subscribeToAll({ filter, fromPosition: START, resolveLinks });
}
```

## Notes

### Everything goes through $all

The reference implementation always uses `subscribeToAll` with a filter. V1's KurrentDB engine uses per-stream reads for `fromStream`/`fromCategory`, but the events are the same - just delivered in a different order for multi-stream cases. For gaffer's purposes, `$all` with a filter is simpler and correct.

A consumer MAY optimize single-stream cases with `subscribeToStream` for V1 if performance matters, but it's not required.

### Prefix matching for event types

KurrentDB V2 uses prefix matching at the read layer. `eventTypeFilter({ prefixes: ["Order"] })` matches `Order`, `OrderPlaced`, `OrderShipped`. The runtime does exact dispatch internally via `ShouldProcess`. Over-subscribing is safe, under-subscribing means missing events.

### V1 fromCategory needs $by_category

When version is `v1` and source is `categories`, the `$ce-{category}` streams must exist. These are populated by the `$by_category` system projection. If it's not enabled, the filter approach still works (matches `order-*` streams in `$all`) but event ordering may differ from production V1.

### System events

The runtime detects and routes `$streamDeleted` and soft-delete `$metadata` events internally. When `handlesDeletedNotifications` is true, these event types are added to the prefix filter so they pass through.

### Link events and $includeLinks

The runtime handles `$includeLinks` filtering internally. When `$includeLinks` is false (default), the runtime drops raw link events (`$>` event type) and resolved-from-link events (events with `linkMetadata` set) in `Feed()`.

V1 subscribes with `resolveLinks: false`, so raw `$>` events appear in the stream. When `$includeLinks: true`, these reach the handler as events with `eventType: "$>"` and `bodyRaw: "0@source-stream"`. When `$includeLinks: false`, the runtime drops them.

V2 subscribes with `resolveLinks: true`, so links are always resolved to their target events before reaching the consumer. The `$includeLinks` option controls whether resolved-from-link events (identified by `linkMetadata` being present) are passed through or dropped.

### reorderEvents / processingLag

Only applies to `fromStreams`. When `info.settings.reorderEvents` is true, buffer events and release in commit position order after `info.settings.processingLag` ms. The runtime does not buffer - the consumer must implement it.

### data must be null, not "null"

When an event has no body, `data` must be `null`, not the 4-character string `"null"`. The runtime treats these differently.

### fromCategory multi-arg edge case

`fromCategory("order")` produces `source.type = "categories"` with `categories: ["order"]`.
`fromCategory(["order", "cart"])` produces `source.type = "streams"` with `streams: ["$ce-order", "$ce-cart"]`.

The reference implementation detects `$ce-` prefixed streams and converts them to category prefix filters.
