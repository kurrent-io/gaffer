# @kurrent/projections-testing

Test KurrentDB projections locally with any test runner (vitest, jest, mocha).

Wraps the [Gaffer runtime](../../runtime/) to execute projections against test events with the same behaviour as a real KurrentDB instance.

## Install

```bash
npm install @kurrent/projections-testing
```

Requires `@kurrent/kurrentdb-client` as a peer dependency.

## Quick start

```typescript
import { createProjection } from "@kurrent/projections-testing";
import { readFile } from "fs/promises";

const source = await readFile("./projections/cart.js", "utf8");
const projection = createProjection<{ count: number }>(source);

// Run over an array of events
for (const { state } of projection.run([
	{
		eventType: "ItemAdded",
		streamId: "cart-1",
		sequenceNumber: 0,
		data: { id: 1 },
	},
	{
		eventType: "ItemAdded",
		streamId: "cart-1",
		sequenceNumber: 1,
		data: { id: 2 },
	},
])) {
	console.log(state); // { count: 1 }, { count: 2 }
}
```

## API

### `createProjection<TState>(source)`

Create a projection from JavaScript source. Does not compile until `validate`, `run`, or `test` is called.

### `projection.validate()`

Compile the projection and return its source definition or an error.

```typescript
const result = projection.validate();
if (result.valid) {
	console.log(result.info.source); // { type: "all" }
	console.log(result.info.events); // ["ItemAdded"] or "all"
} else {
	console.error(result.error);
}
```

### `projection.run(events)`

Run the projection over events, yielding a `StepResult` after each one. Accepts:

- `Iterable<EventInput>` - arrays, generators
- `AsyncIterable<EventInput>` - async generators, client streams
- `KurrentDBClient` - subscribes to the appropriate streams based on the projection's source definition

```typescript
// Sync
for (const { state, emitted, logs } of projection.run(events)) { ... }

// Async
for await (const { state } of projection.run(asyncEvents)) { ... }

// KurrentDB client
for await (const { state } of projection.run(client)) { ... }
```

### `projection.test()`

Create an interactive test session for feeding events one at a time.

```typescript
const test = projection.test();

const step = test.feed({
	eventType: "ItemAdded",
	streamId: "cart-1",
	sequenceNumber: 0,
	data: { id: 1 },
});

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
	data: {},
});
test.feed({
	eventType: "ItemAdded",
	streamId: "cart-2",
	sequenceNumber: 1,
	data: {},
});

test.getState("cart-1"); // state for cart-1
test.getState("cart-2"); // state for cart-2
test.getSharedState(); // shared state (biState projections)
test.getResult("cart-1"); // transformed result (transformBy/filterBy)
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
// Manual test events
{ eventType: 'OrderPlaced', streamId: 'order-1', sequenceNumber: 0, data: { amount: 99 } }

// KurrentDB RecordedEvent (from client)
{ type: 'OrderPlaced', streamId: 'order-1', revision: 0n, ... }

// KurrentDB ResolvedEvent (from subscriptions)
{ event: { type: 'OrderPlaced', streamId: 'order-1', revision: 0n, ... } }
```

`data` and `metadata` accept objects (auto-stringified to JSON) or strings (passed through).

## Errors

When a projection handler throws, a `ProjectionError` is raised with structured properties:

```typescript
try {
	test.feed(event);
} catch (err) {
	if (err instanceof ProjectionError) {
		err.normalized.eventType; // "OrderPlaced"
		err.normalized.streamId; // "order-1"
		err.normalized.data; // '{"amount":99}'
		err.cause; // original runtime error
		err.event; // original input event
	}
}
```
