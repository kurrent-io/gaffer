---
title: CLI
description: Reference for the gaffer command-line interface and gaffer.toml schema.
---

The `gaffer` CLI scaffolds projections, runs them locally against fixtures or live KurrentDB, drives the debugger, and hosts the LSP and MCP servers.

## Commands

| Command                                              | What it does                                                                                                                  |
| ---------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| [`gaffer init`](./commands.md#gaffer-init)           | Create `gaffer.toml` in the current directory.                                                                                |
| [`gaffer scaffold <path>`](./commands.md#gaffer-scaffold) | Create a projection file at `<path>` and register it in `gaffer.toml`.                                                        |
| [`gaffer dev <name>`](./commands.md#gaffer-dev)      | Run a projection against fixtures (`--fixture <name>` or `--events <path>`) or live KurrentDB.                                |
| [`gaffer info <name>`](./commands.md#gaffer-info)    | Print the projection's details: source, partitioning, declared fixtures, engine version, matched events, and any diagnostics. |
| [`gaffer mcp`](./commands.md#gaffer-mcp)             | Start the gaffer MCP server over stdio. See [MCP](./mcp.md).                                                                  |
| [`gaffer lsp`](./commands.md#gaffer-lsp)             | Start the gaffer LSP server over stdio. Used by the [VS Code extension](../extension/vs-code.md).                             |
| [`gaffer config`](./commands.md#gaffer-config)       | Manage user-level configuration (telemetry opt-out, anonymous identity).                                                      |
| [`gaffer version`](./commands.md#gaffer-version)     | Print the gaffer version.                                                                                                     |

See [the full command reference](./commands.md) for every subcommand and flag, or run `gaffer <command> --help`.

## Interactive mode

On a terminal, `gaffer init`, `gaffer scaffold`, and `gaffer dev` prompt for anything you didn't pass on the command line:

- `gaffer init` asks for the engine version.
- `gaffer scaffold` asks for the path (when omitted) and any of source, partitioning, and emit not set via flags.
- `gaffer dev` asks which projection to run (when omitted) and which source to use when none is given via `--events`, `--fixture`, or `--connection`.

Anything you pass explicitly - a positional or a flag - is taken as-is and never re-prompted; only the gaps are asked. Pass `--yes` (`-y`) to skip prompts and accept defaults - the same thing that happens automatically when input isn't a terminal (pipes, CI), so scripts keep working unchanged. Press Ctrl-C or Esc on any prompt to cancel.

![gaffer scaffold prompting for source, name, and emit](/demo-scaffold.gif)

## Project configuration

Each gaffer project has a `gaffer.toml` at its root, created by `gaffer init`. It declares the projections in the project, their entry files, and any named fixtures:

```toml
connection = "kurrentdb://localhost:2113?tls=false"
engine_version = 2

[[projection]]
name = "order-count"
entry = "projections/order-count.js"
fixtures.happy = "fixtures/orders.json"
fixtures.full = "fixtures/orders-full.json"
```

Top-level keys:

- **`connection`**: KurrentDB connection string. Optional; only required when running a projection against a live event stream.
- **`engine_version`**: `1` or `2`. `gaffer init` writes `2` by default; pass `gaffer init --engine-version 1` or pick it at the prompt to write `1`. V1 is for legacy compatibility. Can be overridden per-projection inside `[[projection]]`.

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
- **`--yes` / `-y`**: skip interactive prompts and accept defaults. Applies to `gaffer init`, `gaffer scaffold`, and `gaffer dev`. See [Interactive mode](#interactive-mode).

## Telemetry

The CLI emits anonymous usage telemetry by default. See the [telemetry notice](../telemetry/cli.md) for the full list of what's collected.

Opt out at the user level via any of:

- `gaffer config telemetry off` (persists to the user config file).
- `GAFFER_TELEMETRY_OPTOUT=1` in the environment.
- `KURRENTDB_TELEMETRY_OPTOUT=1` in the environment.
- `DO_NOT_TRACK=1` in the environment.
- VS Code's `telemetry.telemetryLevel` set to `off` (the extension and CLI both respect it).

Opt out at the project level by setting `telemetry = false` in [`gaffer.toml`](../reference/gaffer-toml.md#telemetry).
