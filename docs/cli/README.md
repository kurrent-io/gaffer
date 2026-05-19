---
title: CLI
description: Reference for the gaffer command-line interface and gaffer.toml schema.
order: 2
---

The `gaffer` CLI scaffolds projections, runs them locally against fixtures or live KurrentDB, drives the debugger, and hosts the LSP and MCP servers.

## Commands

| Command                  | What it does                                                                                                                  |
| ------------------------ | ----------------------------------------------------------------------------------------------------------------------------- |
| `gaffer init`            | Create `gaffer.toml`, `.gitignore`, and an empty `.gaffer/` directory.                                                        |
| `gaffer scaffold <name>` | Add a projection JS file and register it in `gaffer.toml`.                                                                    |
| `gaffer dev <name>`      | Run a projection against fixtures (`--fixture <name>` or `--events <path>`) or live KurrentDB.                                |
| `gaffer info <name>`     | Print the projection's details: source, partitioning, declared fixtures, engine version, matched events, and any diagnostics. |
| `gaffer mcp`             | Start the gaffer MCP server over stdio. See [MCP](../mcp/).                                                                   |
| `gaffer lsp`             | Start the gaffer LSP server over stdio. Used by the [VS Code extension](../extension/).                                       |
| `gaffer config`          | Manage user-level configuration (telemetry opt-out, anonymous identity).                                                      |
| `gaffer version`         | Print the gaffer version.                                                                                                     |

Run `gaffer <command> --help` for the full flag set.

## Project configuration

Each gaffer project has a `gaffer.toml` at its root, created by `gaffer init`. It declares the projections in the project, their entry files, and any named fixtures:

```toml
connection = "kurrentdb+discover://localhost:2113"
engine_version = 2

[[projection]]
name = "order-count"
entry = "projections/order-count.js"
fixtures.happy = "fixtures/orders.json"
fixtures.full = "fixtures/orders-full.json"
```

Top-level keys:

- **`connection`**: KurrentDB connection string. Optional; only required when running a projection against a live event stream.
- **`engine_version`**: `1` or `2`. V2 is the current default; V1 is for legacy compatibility. Can be overridden per-projection inside `[[projection]]`.

Per-projection (`[[projection]]`):

- **`name`**: the lookup key for `gaffer dev <name>` and other commands.
- **`entry`**: path to the projection JS file, relative to the project root.
- **`fixtures.<name>`**: path to a JSON events file, relative to the project root. Referenced from `gaffer dev <name> --fixture <fixture-name>`.

## User configuration

User-level settings (telemetry opt-out and a per-install anonymous identity) live in a platform-specific config directory:

- Linux: `$XDG_CONFIG_HOME/gaffer/config.toml` (default `~/.config/gaffer/config.toml`).
- macOS: `~/Library/Application Support/gaffer/config.toml`.
- Windows: `%AppData%\gaffer\config.toml`.

Set `GAFFER_CONFIG_DIR` to override.

Manage with `gaffer config`:

```sh
gaffer config telemetry status   # show current opt-in state and identity
gaffer config telemetry off      # opt out of telemetry
gaffer config telemetry on       # opt back in
```

Project-level telemetry is opted out by setting `telemetry = false` at the top of `gaffer.toml`.

## Common flags

- **`--json`**: structured output instead of the default text rendering. `gaffer dev --json` emits NDJSON (one event per line); other commands emit a single JSON object.
- **`--debug`**: starts the DAP debug server alongside `gaffer dev`. See [Debugging projections](../getting-started/debugging.md).
- **`--connection`**: override `connection` from `gaffer.toml` for a single invocation.
- **`--fixture <name>`** / **`--events <path>`**: pick a named fixture from `gaffer.toml`, or point at a JSON events file directly.

## Telemetry

The CLI emits anonymous usage telemetry by default. See the [telemetry notice](https://telemetry.gaffer.kurrent.io/) for the full list of what's collected, and `gaffer config telemetry off` (or `GAFFER_TELEMETRY_OPTOUT=1`) to disable.
