# Gaffer demo

Example projects showing what Gaffer can do. Each projection demonstrates a different feature of the KurrentDB projection API; the ones with fixtures wired in [`gaffer.toml`](gaffer.toml) replay local event files, no KurrentDB instance required.

## Run a demo

Install the Gaffer CLI:

```sh
npm i -g @kurrent/gaffer
```

Then from this directory:

```sh
gaffer dev order-count --fixture happy
```

The `--fixture` flag picks an event set defined in [`gaffer.toml`](gaffer.toml). The CLI replays those events through the projection and prints the resulting state. Ad-hoc event files work too, via `--events <file>`. Without either, `gaffer dev` subscribes to the project's default environment - the demo's `[env.cloud]` block points at a hosted cluster.

## What's in here

| Projection | Demonstrates |
|---|---|
| [`order-count`](projections/order-count.js) | Per-stream state with `foreachStream`; aggregate counts and totals |
| [`order-notifications`](projections/order-notifications.js) | `log()`, `emit()`, `linkTo()`, error handling |
| [`event-counter`](projections/event-counter.js) | `partitionBy` to count events by type |
| [`bistate-counter`](projections/bistate-counter.js) | Bi-state projections with `$initShared` |
| [`broken`](projections/broken.js) | Syntax errors and how Gaffer reports them |
| [`quirks`](projections/quirks.js) | How `gaffer dev` surfaces compile-time and runtime quirks (`--fixture ticks`) |

Fixtures (`fixtures/orders.json`, `fixtures/orders-full.json`, `fixtures/quirks-ticks.json`) are JSON arrays of event records. Not every projection has one wired - see `gaffer.toml` for which do; the rest take an events file via `--events` or run against a live environment.

## Building from source

If you'd rather run the demo against a local build of the CLI, see the [contributing guide](../CONTRIBUTING.md) for workspace setup, then from the repo root (this directory has its own justfile, so the module recipes don't resolve from here):

```sh
just runtime publish
just cli build
```

and back in this directory:

```sh
../cli/gaffer dev order-count --fixture happy
```

## License

[Apache License 2.0](LICENSE)
