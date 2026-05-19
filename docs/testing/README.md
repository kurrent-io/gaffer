---
title: Testing
description: Drive KurrentDB projections from your test suite with @kurrent/projections-testing.
order: 5
---

`@kurrent/projections-testing` runs projections from inside your existing test suite. Same engine as gaffer and KurrentDB; works with vitest, jest, mocha, or any node test runner.

## Install

```sh
npm install --save-dev @kurrent/projections-testing
```

Requires Node.js 22 or later. `@kurrent/kurrentdb-client` is a peer dependency, needed only when subscribing to a live KurrentDB cluster from a test.

## Quick start

```typescript
import { createProjection } from "@kurrent/projections-testing";
import { readFile } from "fs/promises";

const source = await readFile("./projections/order-count.js", "utf8");
const projection = createProjection<{ count: number; totalCents: number }>(
  source,
);

for (const { state } of projection.run([
  {
    eventType: "OrderPlaced",
    streamId: "order-1",
    sequenceNumber: 0,
    isJson: true,
    data: { cents: 2999 },
  },
  {
    eventType: "OrderPlaced",
    streamId: "order-2",
    sequenceNumber: 1,
    isJson: true,
    data: { cents: 4999 },
  },
])) {
  console.log(state); // { count: 1, totalCents: 2999 }, { count: 2, totalCents: 7998 }
}
```

## API

### `createProjection<TState>(source, options?)`

Create a projection from JavaScript source. The projection compiles lazily on first `validate()`, `run()`, or `test()` call.

Options:

- **`version`**: `"v1"` or `"v2"` (default `"v2"`).
- **`config`**: per-projection settings.
  - `executionTimeoutMs` - max handler execution time per event in ms (default 5000).
- **`databaseConfig`**: database-wide settings.
  - `compilationTimeoutMs` - max compile time in ms (default 5000).
  - `executionTimeoutMs` - default max handler execution time in ms (default 5000).

### `projection.validate()`

Compile the projection and return its source definition. Throws if the source is invalid.

```typescript
const info = projection.validate();
console.log(info.source); // { type: "all" } or { type: "category", value: "order" }
console.log(info.events); // ["OrderPlaced"] or "all"
```

### `projection.run(events)`

Run the projection over events, yielding a `StepResult` after each one. Accepts:

- `Iterable<EventInput>` - arrays, generators.
- `AsyncIterable<EventInput>` - async generators, client streams.
- `KurrentDBClient` - subscribes to the appropriate streams based on the projection's declared source.

```typescript
// Sync
for (const { state, emitted, logs } of projection.run(events)) {
  expect(state.count).toBe(2);
}

// Async
for await (const { state } of projection.run(asyncEvents)) {
  /* ... */
}

// Live KurrentDB
for await (const { state } of projection.run(client)) {
  /* ... */
}
```

### `projection.test()`

Open an interactive test session for feeding events one at a time. Use this when you want to assert against intermediate state rather than only the final aggregate.

```typescript
const test = projection.test();

const step = test.feed({
  eventType: "OrderPlaced",
  streamId: "order-1",
  sequenceNumber: 0,
  isJson: true,
  data: { cents: 2999 },
});

expect(step.state).toEqual({ count: 1, totalCents: 2999 });
expect(step.emitted).toHaveLength(0);
expect(step.logs).toEqual([]);

test.dispose(); // or `using test = projection.test()` for auto-cleanup
```

#### Partitioned state

For projections that partition state (`foreachStream`, `partitionBy`), inspect each partition:

```typescript
test.getState("order-1"); // state for the order-1 partition
test.getState("order-2"); // state for the order-2 partition
test.getSharedState(); // shared state (biState projections)
test.getResult("order-1"); // result for order-1 (V1: post-transform, V2: post-handler state)
```

### `systemEvents`

Helpers for constructing the system events KurrentDB emits (stream deletions, etc.):

```typescript
import { systemEvents } from "@kurrent/projections-testing";

test.feed(systemEvents.streamDeleted("order-1", 5));
```

## Event input

Three event shapes are accepted:

```typescript
// Manual test event (isJson is required)
{ eventType: "OrderPlaced", streamId: "order-1", sequenceNumber: 0, isJson: true, data: { cents: 2999 } }

// KurrentDB RecordedEvent (from client subscriptions)
{ type: "OrderPlaced", streamId: "order-1", revision: 0n, isJson: true, id: "...", created: new Date(), ... }

// KurrentDB ResolvedEvent (from $by_event_type, $by_category, etc.)
{ event: { type: "OrderPlaced", streamId: "order-1", revision: 0n, isJson: true, ... } }
```

`data` and `metadata` accept objects (auto-stringified to JSON) or strings (passed through).

## Errors

Runtime errors propagate as typed `ProjectionError` subclasses with structured fields:

```typescript
import {
  ProjectionHandlerError,
  InvalidProjectionError,
  ProjectionError,
} from "@kurrent/projections-testing";

try {
  test.feed(event);
} catch (err) {
  if (err instanceof ProjectionHandlerError) {
    err.description; // "boom"
    err.event.eventType; // "OrderPlaced"
    err.event.streamId; // "order-1"
    err.event.sequenceNumber; // 42
    err.message; // formatted with source snippet and caret
  }

  if (err instanceof ProjectionError) {
    err.code; // "handler-error", "malformed-event", etc.
    err.description; // human-readable description
  }
}
```

The full error hierarchy: `InvalidProjectionError`, `CompilationTimeoutError`, `InvalidArgumentError`, `ProjectionHandlerError`, `ExecutionTimeoutError`, `MalformedEventError`, `StateSerializationError`, `ProjectionTransformError`.

## See also

- [Your first projection](../getting-started/first-projection.md) - write a projection with gaffer if you haven't yet.
- [CLI](../cli/) - run projections without a test framework.
- [Debugging projections](../getting-started/debugging.md) - step through handlers, inspect state.
