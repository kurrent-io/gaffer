# Source-matching specification

Given a `ProjectionInfo` and an event's `streamId`, this spec defines whether that event would be delivered to the projection's handler by real KurrentDB.

The runtime does no I/O and does not filter by stream: it processes whatever it is fed. Real KurrentDB only delivers events from the streams a projection's source subscribes to. A consumer that supplies events manually, rather than from a live subscription, MUST apply this predicate before `feed()` so the projection sees only the events production would; non-matching events are skipped, not fed.

This is the manual-feed counterpart to [`subscription.md`](./subscription.md): the subscription spec filters at the server (which streams to read); this spec filters in process (whether a given stream matches). The two MUST agree, so an event delivered by a live subscription always passes this predicate.

## Reference implementation

```typescript
import type { ProjectionInfo } from "@kurrent/projections-testing";

function streamMatchesSource(info: ProjectionInfo, streamId: string): boolean {
  switch (info.source.type) {
    case "all":
      // fromAll(): every stream passes. Event-type filtering is the runtime's
      // job (skip reason "unhandled"), not this predicate's.
      return true;

    case "streams": {
      const streams = info.source.streams;
      // fromCategory multi-arg lands all-$ce-<cat> entries here: match by
      // category prefix. Anything else (incl. a mix) is an exact name match,
      // mirroring the subscription filter's branching.
      if (streams.every((s) => s.startsWith("$ce-"))) {
        return streams.some((s) =>
          streamId.startsWith(`${s.slice("$ce-".length)}-`),
        );
      }
      return streams.includes(streamId);
    }

    case "categories":
      // fromCategory("c"): streams in category c are named "c-<id>".
      return info.source.categories.some((c) => streamId.startsWith(`${c}-`));
  }
}
```

## Cases

- `fromStream("X")` — only `streamId === "X"` passes.
- `fromStreams(["X", "Y"])` — only `streamId === "X"` or `"Y"` passes.
- `fromCategory("c")` — only `streamId` starting with `c-` passes (the category-stream pattern). The multi-arg form surfaces as `$ce-c` entries in `source.streams` and matches the same way.
- `fromAll()` — every stream passes.

## Notes

### Category matching is prefix-based, not exact

KurrentDB's default category separator is `-`: a stream `order-1` belongs to category `order`. A `fromCategory("order")` projection reads every `order-*` stream, so the predicate matches on the `order-` prefix rather than an exact name. This mirrors the `streamNameFilter({ prefixes: ["order-"] })` the subscription spec builds.

### Why this lives outside the runtime

The runtime is puppetable and stream-agnostic by design: it feeds whatever it is handed. Stream selection belongs to the subscription layer. Without a live subscription there is no such layer, so a consumer reconstructs the decision from `ProjectionInfo` (the `@kurrent/projections-testing` `feed()` path does this, surfacing a mismatch as `reason: "wrong-stream"`). A live `$all` subscription gets the same guarantee from the server-side filter in [`subscription.md`](./subscription.md).
