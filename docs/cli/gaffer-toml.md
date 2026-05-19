---
title: gaffer.toml
description: Full reference for the gaffer.toml project configuration file.
order: 3
---

`gaffer.toml` lives at the root of a gaffer project and declares its projections, connection settings, and engine version. `gaffer init` writes the initial file.

## Example

```toml
connection = "kurrentdb://localhost:2113?tls=false"
engine_version = 2
telemetry = true

[[projection]]
name = "order-count"
entry = "projections/order-count.js"
fixtures.happy = "fixtures/orders.json"
fixtures.full = "fixtures/orders-full.json"
```

## Top-level keys

### `connection`

```toml
connection = "kurrentdb://localhost:2113?tls=false"
```

KurrentDB connection string. Used when running a projection against a live event stream (`gaffer dev <projection>` without `--events` or `--fixture`). Override per-invocation with `--connection`.

Optional. Omit when you only run projections against fixture files.

### `engine_version`

```toml
engine_version = 2
```

Projection engine version: `1` or `2`. `gaffer init` writes `2`. V1 is for legacy compatibility with older KurrentDB releases.

A projection that doesn't set its own `engine_version` inherits this one. Loading `gaffer.toml` fails if neither the top-level nor the projection sets a version.

### `db_version`

```toml
db_version = "26.1.0"
```

Target KurrentDB version (`MAJOR.MINOR.PATCH`). Gaffer uses this to opt out of engine quirks that have been fixed in the named version or later. Unset means gaffer matches every known KurrentDB quirk.

The `GAFFER_DB_VERSION` environment variable overrides every `db_version` in the file. Useful for CI matrices.

Optional.

### `compilation_timeout` / `execution_timeout`

```toml
compilation_timeout = 5000
execution_timeout = 5000
```

Time limits in milliseconds. `compilation_timeout` bounds projection compilation. `execution_timeout` bounds each handler invocation. The runtime applies a 5000ms default for both when omitted.

Per-projection overrides via `execution_timeout` inside `[[projection]]`.

### `telemetry`

```toml
telemetry = false
```

Project-level telemetry opt-out. Setting `false` disables telemetry for any gaffer command run inside this project, regardless of the user's own opt-out state.

User-level opt-outs that apply across all projects: `gaffer config telemetry off`, `GAFFER_TELEMETRY_OPTOUT`, `KURRENTDB_TELEMETRY_OPTOUT`, `DO_NOT_TRACK`, and VS Code's `telemetry.telemetryLevel`.

Optional. Telemetry is on by default.

## Per-projection keys

Each projection is a `[[projection]]` table-array entry.

### `name`

```toml
name = "order-count"
```

Lookup key for `gaffer dev <name>`, `gaffer info <name>`, the VS Code lens, and MCP tools. Required. Names must be unique within a project.

### `entry`

```toml
entry = "projections/order-count.js"
```

Path to the projection's JavaScript source file, relative to the project root. Required.

### `fixtures.<name>`

```toml
fixtures.happy = "fixtures/orders.json"
fixtures.full = "fixtures/orders-full.json"
```

Named JSON events files. Run with `gaffer dev <name> --fixture happy`. Path is relative to the project root.

Optional. Add one entry per scenario you want to re-run.

### `engine_version` (per-projection)

```toml
[[projection]]
name = "legacy-counter"
entry = "projections/legacy-counter.js"
engine_version = 1
```

Per-projection override of the top-level `engine_version`. Useful when one projection in a project targets a different engine than the rest.

Optional.

### `db_version` (per-projection)

```toml
[[projection]]
name = "v26-only"
entry = "projections/v26-only.js"
db_version = "26.1.0"
```

Per-projection override of the top-level `db_version`. Optional.

### `execution_timeout` (per-projection)

```toml
[[projection]]
name = "slow-projection"
entry = "projections/slow-projection.js"
execution_timeout = 30000
```

Per-projection override of the top-level `execution_timeout`. Use for projections with long-running handlers (large reductions, heavy regex work). Optional.

## Resolution order

Settings that exist at both top-level and per-projection resolve from most-specific to least:

| Setting               | Resolution                                                           |
| --------------------- | -------------------------------------------------------------------- |
| `engine_version`      | Per-projection > top-level. Required (load fails if neither is set). |
| `db_version`          | `GAFFER_DB_VERSION` env > per-projection > top-level > unset.        |
| `execution_timeout`   | Per-projection > top-level > 5000ms.                                 |
| `compilation_timeout` | Top-level only > 5000ms.                                             |
| `connection`          | `--connection` flag > top-level.                                     |
