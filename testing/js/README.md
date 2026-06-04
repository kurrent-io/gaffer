# @kurrent/projections-testing

Test [KurrentDB](https://www.kurrent.io) projections locally with any test runner (vitest, jest, mocha).

Wraps the [gaffer runtime](../../runtime/) to execute projections against test events with the same behaviour as a real KurrentDB instance.

## Install

```sh
npm install --save-dev @kurrent/projections-testing
```

Requires Node.js 22 or later. `@kurrent/kurrentdb-client` is a peer dependency.

## Quick start

Run a projection over an array of events:

```typescript
import { createProjection } from "@kurrent/projections-testing";
import { readFile } from "fs/promises";

const source = await readFile("./projections/cart.js", "utf8");
const projection = createProjection<{ count: number }>(source, {
	engineVersion: 2,
});

for (const result of projection.run([
	{
		eventType: "ItemAdded",
		streamId: "cart-1",
		sequenceNumber: 0,
		isJson: true,
		data: { id: 1 },
	},
	{
		eventType: "ItemAdded",
		streamId: "cart-1",
		sequenceNumber: 1,
		isJson: true,
		data: { id: 2 },
	},
])) {
	if (result.status !== "processed") continue;
	console.log(result.state); // { count: 1 }, { count: 2 }
}
```

## API

### `createProjection<TState>(source, options)`

Create a projection from JavaScript source. Does not compile until `validate`, `run`, or `test` is called.

Options:

- `engineVersion` - `1` or `2`. Required.
- `quirksVersion` - target KurrentDB version (`"MAJOR.MINOR.PATCH"`, e.g. `"26.1.0"`). Unset (the default) reproduces every known engine quirk; set a version to turn off quirks fixed upstream as of that version.
- `config` - per-projection settings
  - `executionTimeoutMs` - max handler execution time per event in ms (default 5000)
- `databaseConfig` - database-wide settings
  - `compilationTimeoutMs` - max compilation time in ms (default 5000)
  - `executionTimeoutMs` - default max handler execution time in ms (default 5000)

### `projection.validate()`

Compile the projection and return its source definition. Throws if the source is invalid.

```typescript
const info = projection.validate();
console.log(info.source); // { type: "all" }
console.log(info.events); // ["ItemAdded"] or "all"
```

### `projection.run(events)`

Run the projection over events, yielding a `StepResult` after each one. Accepts:

- `Iterable<EventInput>` - arrays, generators
- `AsyncIterable<EventInput>` - async generators, client streams
- `KurrentDBClient` - subscribes to the appropriate streams based on the projection's source definition

`StepResult` is a discriminated union on `status`. Both shapes carry `event` and `status`. The `processed` shape adds `state`, `stateRaw`, `result`, `sharedState`, `emitted`, `logs`, and `diagnostics`. The `skipped` shape adds `reason` explaining why (`unhandled`, `non-json`, `link`, `no-partition`, `no-delete-handler`, `wrong-stream`). Guard before destructuring:

```typescript
for (const result of projection.run(events)) {
	if (result.status !== "processed") continue;
	// result.state, result.emitted, result.logs
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

// KurrentDB client (unbounded subscription; break out when done)
for await (const result of projection.run(client)) {
	if (result.status === "processed") {
		/* ... */
	}
}
```

### `projection.test()`

Create an interactive test session for feeding events one at a time.

```typescript
const test = projection.test();

const step = test.feed({
	eventType: "ItemAdded",
	streamId: "cart-1",
	sequenceNumber: 0,
	isJson: true,
	data: { id: 1 },
});

if (step.status !== "processed") {
	throw new Error(`expected processed, got ${step.status}: ${step.reason}`);
}

expect(step.state).toEqual({ count: 1 });
expect(step.emitted).toHaveLength(0);
expect(step.logs).toEqual([]);

test.dispose(); // or use `using test = projection.test()`
```

#### Querying state

For partitioned projections, query state by partition:

```typescript
test.feed({
	eventType: "ItemAdded",
	streamId: "cart-1",
	sequenceNumber: 0,
	isJson: true,
	data: {},
});
test.feed({
	eventType: "ItemAdded",
	streamId: "cart-2",
	sequenceNumber: 1,
	isJson: true,
	data: {},
});

test.getState("cart-1"); // state for cart-1
test.getStateRaw("cart-1"); // raw persisted state JSON, before parse (see Serialization quirks)
test.getState("cart-2"); // state for cart-2
test.getSharedState(); // shared state (biState projections)
test.getResult("cart-1"); // result for cart-1 (V1: post-transform; V2: post-handler state)
```

#### Serialization quirks

Some KurrentDB quirks only show up in how state is persisted, and `state` / `getState()` hide them by parsing the persisted JSON on read.

- **`step.diagnostics`** lists the quirks encountered while processing the event (empty when none; it can carry more than one, and the same code can repeat). The motivating case is `quirk.serialize.rawString`: a bare string state that isn't valid JSON is persisted un-encoded, so the projection would fault when it reloads. Non-persistence quirks appear here too, such as `quirk.log.multiParam` fired at each multi-argument `log()` call.
- **`step.stateRaw`** and **`getStateRaw(partition?)`** return the persisted state JSON string before `JSON.parse`, so you can assert the exact persisted value.

```typescript
const test = createProjection(
	`fromAll().when({ Set: (s, e) => e.body.name })`,
	{
		engineVersion: 2,
	},
).test();
const step = test.feed({
	eventType: "Set",
	streamId: "s-1",
	sequenceNumber: 0,
	isJson: true,
	data: { name: "alice" },
});
if (step.status !== "processed") throw new Error(step.reason);

expect(step.state).toBe("alice"); // parsed
expect(step.stateRaw).toBe('"alice"'); // persisted, JSON-encoded
expect(step.diagnostics.map((d) => d.code)).toContain(
	"quirk.serialize.rawString",
);
```

### `systemEvents`

Helpers for constructing KurrentDB system events:

```typescript
import { systemEvents } from "@kurrent/projections-testing";

test.feed(systemEvents.streamDeleted("cart-123", 5));
```

## Event input

Three event shapes are accepted:

```typescript
// Manual test events (isJson is required)
{ eventType: 'OrderPlaced', streamId: 'order-1', sequenceNumber: 0, isJson: true, data: { amount: 99 } }

// KurrentDB RecordedEvent (from client)
{ type: 'OrderPlaced', streamId: 'order-1', revision: 0n, isJson: true, id: '...', created: new Date(), ... }

// KurrentDB ResolvedEvent (from subscriptions)
{ event: { type: 'OrderPlaced', streamId: 'order-1', revision: 0n, isJson: true, ... } }
```

`data` and `metadata` accept objects (auto-stringified to JSON) or strings (passed through).

For manual test events, `eventType` and `streamId` must be non-empty and `sequenceNumber` must be a non-negative integer, matching what KurrentDB can actually deliver to a handler.

Events are matched against the projection's declared source: a `fromStream("a")` / `fromStreams` / `fromCategory` projection only processes events on streams it subscribes to, and others are skipped with `reason: "wrong-stream"` (mirroring KurrentDB delivery). `fromAll()` accepts every stream.

## Errors

Errors from the runtime propagate as typed `ProjectionError` subclasses with structured fields and formatted messages:

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

	// or catch all projection errors
	if (err instanceof ProjectionError) {
		err.code; // "handler-error", "malformed-event", etc.
		err.description; // human-readable description
		err.diagnostics; // quirks that fired on the throwing event (e.g. quirk.serialize.nonFinite)
	}
}
```

When a quirk throws, `err.diagnostics` carries it (and any quirk that fired earlier in the same event), the same `Diagnostic` shape as `step.diagnostics` on a processed step - so you assert on a throwing quirk the same way.

## Related packages

| Package                                                                           | What it is                                                 |
| --------------------------------------------------------------------------------- | ---------------------------------------------------------- |
| [`@kurrent/gaffer`](https://www.npmjs.com/package/@kurrent/gaffer)                | CLI to scaffold, run, debug, and deploy projections        |
| [KurrentDB Projections for VS Code](https://gaffer.kurrent.io/extension/vs-code/) | Editor integration with debugger, codelens, and MCP server |

## Documentation

Full documentation at <https://gaffer.kurrent.io/testing/nodejs/>.

Bugs go to [GitHub Issues](https://github.com/kurrent-io/gaffer/issues). Questions and feature requests to [Discussions](https://github.com/kurrent-io/gaffer/discussions).

## License

[Apache License 2.0](LICENSE). Depends on `@kurrent/gaffer-runtime`, which is distributed under the [Kurrent License v1](https://github.com/kurrent-io/gaffer/blob/main/LICENSE.md).
