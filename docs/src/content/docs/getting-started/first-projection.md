---
title: Your first projection
description: Write, run, and iterate on a KurrentDB projection locally with gaffer, no running database needed.
---

A projection is server-side JavaScript that KurrentDB runs over a stream of events to derive new streams or aggregated state. Gaffer runs the same JavaScript engine KurrentDB uses, so the projection you write here is the projection that ships.

## Before you start

You need [`@kurrent/gaffer`](https://www.npmjs.com/package/@kurrent/gaffer) on your PATH and Node.js 22 or later. See [Install](./install.md#install-the-cli) if you don't have it yet.

## Initialise a project

In an empty directory:

```sh
gaffer init
```

This creates a starter `gaffer.toml` in the current directory. It's a commented template with no environments or projections yet; you fill it in by scaffolding projections (below) and adding an [`[env.<name>]`](../reference/gaffer-toml.md#envname) block when you're ready to run against a live database. See the [`gaffer.toml` reference](../reference/gaffer-toml.md) for the full schema.

## Scaffold a projection

```sh
gaffer scaffold projections/order-count.js
```

This creates the file at the path you gave and registers it in `gaffer.toml` under the basename (`order-count`). On a terminal you'll first be prompted for the source, partitioning, emit, and engine version. Press Enter at each to accept the defaults (which produce the skeleton below), or add `-y` to skip the prompts. The scaffolded file is a working skeleton with no logic yet:

![gaffer scaffold prompting for projection options](/demo-scaffold.gif)

<!-- prettier-ignore -->
```js
fromAll()
  .when({
    $init() {
      return {};
    },
    // Add your event handlers here
    // EventType(state, event) {
    //   return state;
    // }
  })
```

Two pieces to know:

- **`fromAll()`**: selects every event in the database. Other selectors (`fromStream`, `fromCategory`) target a specific stream or category.
- **`.when({...})`**: the handler map. `$init` returns the projection's initial state. Every other key is an event-type handler that receives the current state and the incoming event, and returns the new state.

:::tip
Handlers must return state. If a handler returns `undefined`, state becomes `undefined` on the next call and the projection silently breaks. The runtime treats `return null` as an explicit reset.
:::

## Make it count

Replace the body with a counter for `OrderPlaced` events:

```js
fromAll().when({
  $init() {
    return { count: 0, totalCents: 0 };
  },
  OrderPlaced(state, event) {
    state.count += 1;
    state.totalCents += event.body.cents;
    return state;
  },
});
```

`event.body` is the parsed JSON body of the event. Handlers run once per matching event in stream order. State persists across calls within the same projection run.

## Add some test events

Save <a href="/orders.json" download>orders.json</a> to `fixtures/orders.json` in your project, or copy the contents below:

```json
[
  {
    "eventType": "OrderPlaced",
    "streamId": "order-1",
    "data": "{\"cents\": 2999, \"item\": \"Widget\"}"
  },
  {
    "eventType": "OrderPlaced",
    "streamId": "order-2",
    "data": "{\"cents\": 4999, \"item\": \"Gadget\"}"
  },
  {
    "eventType": "OrderShipped",
    "streamId": "order-1",
    "data": "{\"trackingId\": \"TRK-001\"}"
  }
]
```

Three events: two orders, one `OrderShipped`. The projection should ignore the third because there's no handler for that event type.

## Run it

```sh
gaffer dev order-count --events fixtures/orders.json
```

Gaffer replays each event through the projection and prints the resulting state along the way. After the last event, the summary shows the final state:

```
State: { "count": 2, "totalCents": 7998 }
```

The `OrderShipped` event flowed through and was skipped - no handler, no state change.

## Iterate

Add a second handler for `OrderShipped` that tracks shipment status:

```js
fromAll().when({
  $init() {
    return { count: 0, totalCents: 0, shipped: 0 };
  },
  OrderPlaced(state, event) {
    state.count += 1;
    state.totalCents += event.body.cents;
    return state;
  },
  OrderShipped(state) {
    state.shipped += 1;
    return state;
  },
});
```

Re-run the same command. The final state is now:

```
State: { "count": 2, "totalCents": 7998, "shipped": 1 }
```

The fixture didn't change, but the new handler ran against the existing `OrderShipped` event in it. Gaffer reruns the projection from scratch each time, so iteration is fast and deterministic.

## Name the fixture

Typing the events path each run gets old. Declare the fixture once in `gaffer.toml`, alongside the projection block `gaffer scaffold` added earlier:

```toml
[[projection]]
name = "order-count"
entry = "projections/order-count.js"
fixtures.happy = "fixtures/orders.json"
```

Then drop `--events` for `--fixture`:

```sh
gaffer dev order-count --fixture happy
```

Use named fixtures for scenarios you'll re-run (happy path, edge cases). `--events` stays for one-off paths.

## See also

- **Step through with the debugger**: see [Debugging projections](./debugging.md) for the VS Code extension setup and other editor wireups.
- **Test from your test suite**: drive projections directly from vitest, jest, or mocha with [`@kurrent/projections-testing`](../testing/nodejs.md).
- **Use an AI assistant**: point Claude Code, Cursor, Continue, or Copilot at `gaffer mcp` for scaffolding, validation, and debugging tools - see [MCP](../cli/mcp.md).
- **Partition state per stream**: `foreachStream()` between `fromAll()` and `.when()` gives each stream its own state slice. Useful when you're aggregating per-entity instead of globally.
- **Emit derived events**: `emit('stream-name', 'EventType', { ...data })` from inside a handler writes a new event to a target stream. The basis for read-model projections and continuous queries.
- **The full projection API**: `partitionBy`, `outputState`, `transformBy`, `filterBy`, `linkTo`, and the `$init`/`$any`/`$deleted`/`$created` system handlers. See the [projection API reference](https://docs.kurrent.io/server/v26.1/features/projections/custom).
