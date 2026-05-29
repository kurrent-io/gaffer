---
title: Node.js
description: Drive KurrentDB projections from your test suite with @kurrent/projections-testing.
---

`@kurrent/projections-testing` runs projections from inside your existing test suite on the same engine as gaffer and KurrentDB. Works with vitest, jest, mocha, or any node test runner.

## Install

```sh
npm install --save-dev @kurrent/projections-testing
```

`@kurrent/kurrentdb-client` is a peer dependency, needed only when subscribing to a live KurrentDB cluster from a test.

## Requirements

- **Node.js 22 or later**, enforced through the package's `engines` field.
- **ESM.** The package is ESM-only, so your project needs `"type": "module"` in `package.json`.
- **TypeScript 5.2 or later** if you use TypeScript. The API relies on `using` (explicit resource management) for automatic session cleanup.
- A **`tsconfig.json`** with at least:
  - `"target": "ES2022"` or later.
  - `"lib": ["ES2022", "ESNext.Disposable"]`. The `ESNext.Disposable` entry is required for `using test = projection.test()`; without it TypeScript fails with a cryptic error about `Symbol.dispose` not existing rather than a missing-lib hint.
  - `"moduleResolution": "Node16"`, `"NodeNext"`, or `"Bundler"`.

No special test-runner configuration is needed; it works with vitest, jest, mocha, or `node --test` as-is.

## Quick start

Given a projection that counts orders and sums their value:

```javascript
// projections/order-count.js
fromAll().when({
  $init: () => ({ count: 0, totalCents: 0 }),
  OrderPlaced: (state, event) => ({
    count: state.count + 1,
    totalCents: state.totalCents + event.body.cents,
  }),
});
```

Load its source and run events through it:

```typescript
import { createProjection } from "@kurrent/projections-testing";
import { readFile } from "fs/promises";

const source = await readFile("./projections/order-count.js", "utf8");
const projection = createProjection<{ count: number; totalCents: number }>(
  source,
  { engineVersion: 2 },
);

for (const result of projection.run([
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
  if (result.status !== "processed") continue;
  console.log(result.state); // { count: 1, totalCents: 2999 }, { count: 2, totalCents: 7998 }
}
```

## API

### `createProjection<TState>(source, options)`

Create a projection from JavaScript source. The projection compiles lazily on first `validate()`, `run()`, or `test()` call.

Options:

- **`engineVersion`**: `1` or `2`. Required.
- **`quirksVersion`**: target KurrentDB version (`"MAJOR.MINOR.PATCH"`, e.g. `"26.1.0"`). Unset (the default) reproduces every known engine quirk; set a version to turn off quirks fixed upstream as of that version.
- **`config`**: per-projection settings.
  - `executionTimeoutMs`: max handler execution time per event in ms (default 5000).
- **`databaseConfig`**: database-wide settings.
  - `compilationTimeoutMs`: max compile time in ms (default 5000).
  - `executionTimeoutMs`: default max handler execution time in ms (default 5000).

### `projection.validate()`

Compile the projection and return its source definition. Throws if the source is invalid.

```typescript
const info = projection.validate();
console.log(info.source); // { type: "all" } or { type: "categories", categories: ["order"] }
console.log(info.events); // ["OrderPlaced"] or "all"
```

### `projection.run(events)`

Run the projection over events, yielding a `StepResult` after each one. Accepts:

- `Iterable<EventInput>` - arrays, generators.
- `AsyncIterable<EventInput>` - async generators, client streams.
- `KurrentDBClient` - subscribes to the appropriate streams based on the projection's declared source.

`StepResult` is a discriminated union on `status` (see [Step results](#step-results) for the full field list). Guard before destructuring:

```typescript
for (const result of projection.run(events)) {
  if (result.status !== "processed") continue;
  expect(result.state.count).toBe(2);
}
```

Async and live-client paths look the same:

```typescript
// Async
for await (const result of projection.run(asyncEvents)) {
  if (result.status === "processed") {
    /* ... */
  }
}
```

#### Against live KurrentDB

Pass a `KurrentDBClient` and the projection subscribes to the streams its source declares (`fromAll`, `fromCategory("order")`, etc.). The subscription is unbounded - break out of the loop when an assertion holds or a fixed number of events have flowed through.

```typescript
import { KurrentDBClient, jsonEvent } from "@kurrent/kurrentdb-client";
import { createProjection } from "@kurrent/projections-testing";
import { readFile } from "fs/promises";

const client = KurrentDBClient.connectionString(
  "kurrentdb://localhost:2113?tls=false",
);

// Seed some events
await client.appendToStream("order-1", [
  jsonEvent({ type: "OrderPlaced", data: { cents: 2999 } }),
  jsonEvent({ type: "OrderPlaced", data: { cents: 4999 } }),
]);

const source = await readFile("./projections/order-count.js", "utf8");
const projection = createProjection<{ count: number; totalCents: number }>(
  source,
  { engineVersion: 2 },
);

let final;
for await (const result of projection.run(client)) {
  if (result.status !== "processed") continue;
  final = result.state;
  if (final.count >= 2) break;
}

expect(final).toEqual({ count: 2, totalCents: 7998 });

await client.dispose();
```

Breaking out of the loop disposes the subscription cleanly.

:::caution[Connecting over TLS]
When pointing at a TLS cluster with a self-signed or private-CA certificate, `?tlsVerifyCert=false` in the connection string is silently ignored by `@kurrent/kurrentdb-client`. Trust the certificate explicitly instead: pass `tlsCAFile=<path>` in the connection string, or set the `NODE_EXTRA_CA_CERTS` environment variable to the CA file.
:::

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

if (step.status !== "processed") {
  throw new Error(`expected processed, got ${step.status}: ${step.reason}`);
}

expect(step.state).toEqual({ count: 1, totalCents: 2999 });
expect(step.emitted).toHaveLength(0);
expect(step.logs).toEqual([]);

test.dispose(); // or `using test = projection.test()` for auto-cleanup
```

#### Partitioned state

For projections that partition state (`foreachStream`, `partitionBy`), inspect each partition:

```typescript
test.getState("order-1"); // state for the order-1 partition
test.getStateRaw("order-1"); // raw persisted state JSON, before parse (see Raw state and diagnostics)
test.getState("order-2"); // state for the order-2 partition
test.getSharedState(); // shared state (biState projections)
test.getResult("order-1"); // result for order-1 (V1: post-transform, V2: post-handler state)
```

#### Raw state and diagnostics

Some KurrentDB quirks only surface in how state is persisted, and `state` / `getState()` hide them by parsing the persisted JSON on read (see also [State serialization](#state-serialization)).

- **`step.diagnostics`** lists the quirks encountered while processing the event (empty when none; it can carry more than one, and the same code can repeat). The motivating case is biState string slots: KurrentDB JSON-quotes a raw string written to a state slot (`compat.biState.stringSlot` for the main slot, `compat.biState.sharedStringSlot` for shared state), so `"hello"` persists as `"\"hello\""`. Non-persistence quirks appear here too, such as `compat.log.multiParam` fired at each multi-argument `log()` call.
- **`step.stateRaw`** and **`getStateRaw(partition?)`** return the persisted state JSON string before `JSON.parse`, so you can assert against the double-quoted value the quirk produces.

```typescript
const step = test.feed({
  eventType: "SetName",
  streamId: "s-1",
  sequenceNumber: 0,
  isJson: true,
  data: { name: "alice" },
});
if (step.status !== "processed") throw new Error(step.reason);

expect(step.state).toBe("alice"); // parsed - quirk hidden
expect(step.stateRaw).toBe('"alice"'); // raw - double-quoting visible
expect(step.diagnostics.map((d) => d.code)).toContain(
  "compat.biState.stringSlot",
);
```

### `systemEvents`

Helpers for constructing the system events KurrentDB emits (stream deletions, etc.):

```typescript
import { systemEvents } from "@kurrent/projections-testing";

test.feed(systemEvents.streamDeleted("order-1", 5));
```

## Step results

`run()` and `test().feed()` both yield a `StepResult`, a discriminated union on `status`. Narrow on `status` before reading the processed-only fields.

A **`processed`** result (the handler ran) carries:

| Field         | Description                                                                                                                              |
| ------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| `state`       | Parsed projection state for the affected partition.                                                                                      |
| `stateRaw`    | The persisted state JSON string before `JSON.parse`, or `null` when no state was produced. See [State serialization](#state-serialization). |
| `result`      | The partition's result. V1: state after `transformBy`/`filterBy`, or `state` if no transform applies. V2: post-handler state; transforms are not invoked. |
| `sharedState` | Shared state for biState projections; `undefined` otherwise.                                                                             |
| `partition`   | The partition key that was updated. Absent for unpartitioned projections.                                                                |
| `event`       | The input event, round-tripped verbatim, so you can assert against it.                                                                   |
| `emitted`     | Events emitted during processing (`emit` / `linkTo`).                                                                                     |
| `logs`        | Messages from `log()` calls.                                                                                                              |
| `diagnostics` | Quirks that fired while processing this event, empty when none. See [State serialization](#state-serialization).                         |

A **`skipped`** result (the event never reached the handler) carries the same `event` plus a `reason`: `unhandled`, `non-json`, `link`, `no-partition`, `no-delete-handler`, or `wrong-stream`.

## State serialization

Projection state is persisted as JSON by the same engine KurrentDB uses, so a few JavaScript values serialize in ways worth knowing when you assert against `state`:

- **`BigInt`** serializes to a decimal string: `10n` persists as `"10"`, matching KurrentDB.
- **`undefined`** object properties are dropped, exactly like `JSON.stringify`; in an array position `undefined` becomes `null`.
- **`NaN` and `Infinity`** throw a `StateSerializationError`, because KurrentDB rejects them (the `compat.serialize.nonFinite` quirk).

`state` and `getState()` hide the persisted form by parsing it on read. To assert against the raw JSON (including quirks like a biState string slot being double-quoted), read `stateRaw` / `getStateRaw()` and inspect `diagnostics` (see [Raw state and diagnostics](#raw-state-and-diagnostics)).

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

For manual test events, `eventType` and `streamId` must be non-empty and `sequenceNumber` must be a non-negative integer, matching what KurrentDB can actually deliver to a handler.

The `created` timestamp on events from KurrentDB carries sub-millisecond precision: a 7-digit fractional second (.NET round-trip `"o"` format). Parse it with `new Date(event.created)` for assertions. Some strict ISO-8601 parsers reject the extra digits.

Events are matched against the projection's declared source: a `fromStream("a")` / `fromStreams` / `fromCategory` projection only processes events on streams it subscribes to, and others are skipped with `reason: "wrong-stream"` (mirroring KurrentDB delivery). `fromAll()` accepts every stream.

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
- [CLI](../cli/index.md) - run projections without a test framework.
- [Debugging projections](../getting-started/debugging.md) - step through handlers, inspect state.
