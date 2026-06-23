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
| [`gaffer info <name>`](./commands.md#gaffer-info)    | Print the projection's details: source, partitioning, declared fixtures, engine version, matched events, whether it emits events, and any diagnostics. |
| [`gaffer diff <projection>`](./commands.md#gaffer-diff) | Compare a projection's local definition against what's deployed: in sync, drifted, not deployed, untracked, or invalid (the local source doesn't compile). |
| [`gaffer status [projection]`](./commands.md#gaffer-status) | Show the runtime state of projections on an environment and how they compare to local config. |
| [`gaffer deploy [projection]`](./commands.md#gaffer-deploy) | Create or update projections on an environment: create the new ones, update the changed ones, skip the in-sync ones. Compiles every projection locally first and refuses the run if any has errors (`--no-validate` bypasses). Shows the plan and confirms before applying (`--yes` skips); a production server is extra-guarded and refuses `--no-validate`. |
| [`gaffer auth`](./commands.md#gaffer-auth)           | Sign in to an environment's OAuth identity provider and store the token. See [`[env.<name>.oauth]`](../reference/gaffer-toml.md#envnameoauth). |
| [`gaffer mcp`](./commands.md#gaffer-mcp)             | Start the gaffer MCP server over stdio. See [MCP](./mcp.md).                                                                  |
| [`gaffer lsp`](./commands.md#gaffer-lsp)             | Start the gaffer LSP server over stdio. Used by the [VS Code extension](../extension/vs-code.md).                             |
| [`gaffer config`](./commands.md#gaffer-config)       | Manage user-level configuration (telemetry opt-out, anonymous identity).                                                      |
| [`gaffer version`](./commands.md#gaffer-version)     | Print the gaffer version.                                                                                                     |

See [the full command reference](./commands.md) for every subcommand and flag, or run `gaffer <command> --help`.

## Interactive mode

On a terminal, `gaffer scaffold` and `gaffer dev` prompt for anything you didn't pass on the command line:

- `gaffer scaffold` asks for the path (when omitted) and any of source, partitioning, emit, and engine version not set via flags.
- `gaffer dev` asks which projection to run (when omitted) and which event source to use when none is pinned via `--events`, `--fixture`, `--connection`, or `--env`. The picker lists every declared fixture and configured environment; with a single source it's used without asking.

Anything you pass explicitly (a positional or a flag) is taken as-is and never re-prompted; only the gaps are asked. Pass `--yes` (`-y`) to skip prompts and accept defaults, the same thing that happens automatically when input isn't a terminal (pipes, CI), so scripts keep working unchanged. Press Ctrl-C or Esc on any prompt to cancel.

![gaffer scaffold prompting for projection options](/demo-scaffold.gif)

## Project configuration

Each gaffer project has a `gaffer.toml` at its root, created by `gaffer init`. It declares the projections in the project, their entry files, and any named fixtures:

```toml
[env.local]
connection = "kurrentdb://localhost:2113?tls=false"
default = true

[[projection]]
name = "order-count"
entry = "projections/order-count.js"
engine_version = 2
fixtures.happy = "fixtures/orders.json"
fixtures.full = "fixtures/orders-full.json"
```

Top-level keys:

- **`[env.<name>]`**: an environment, naming a KurrentDB connection. Each block has a required **`connection`** (the connection string, supporting `${VAR}` expansion so credentials can stay out of the file) and an optional **`default`** bool. Exactly one environment may be the default. Select an environment with `gaffer dev --env <name>` or pick it from the interactive prompt; `--env` can be omitted on a non-interactive run when one environment is the default. An environment can also authenticate with OAuth, an X.509 client certificate, or basic credentials; see [Authentication](./authentication.md). See [Environment file](#environment-file-env) and the [gaffer.toml reference](../reference/gaffer-toml.md#envname).

`engine_version` is set per-`[[projection]]` (`1` or `2`), not at the top level.

Per-projection (`[[projection]]`):

- **`name`**: the lookup key for `gaffer dev <name>` and other commands.
- **`entry`**: path to the projection JS file, relative to the project root.
- **`engine_version`**: `1` or `2`. Required on every projection. V1 is for legacy compatibility; V2 is the default for new projections.
- **`fixtures.<name>`**: path to a JSON events file, relative to the project root. Referenced from `gaffer dev <name> --fixture <fixture-name>`.

## Environment file (`.env`)

A `.env` file at the project root is loaded into the environment when gaffer starts, so secrets stay out of `gaffer.toml` and out of version control. Reference them in an environment's `connection` with `${VAR}`:

```toml
[env.local]
connection = "kurrentdb://admin:${DB_PASSWORD}@localhost:2113"
```

`.env` supplies any environment variable gaffer reads, including the telemetry and update-check opt-outs below.

A per-environment `.env.<env>` file (matching the selected `[env.<name>]`) overlays the base `.env`, so each environment can carry its own credentials. The precedence, highest first, is the shell environment, then `.env.<env>`, then the base `.env`. A variable set in your shell, or injected by CI, is never overwritten by either file.

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

- **`--json`**: structured output instead of the default text rendering. `gaffer dev --json` emits NDJSON, one object per line, each tagged with a `type`: `info`, `event`, `result`, `error`, `fatal_error`, `summary`, `auth_required`, and `run_error`. `auth_required` signals that a live run needs an interactive sign-in, and `run_error` that a run ended on a connection failure. `gaffer status --json` and `gaffer deploy --json` each emit a JSON array, one object per projection; other commands emit a single JSON object. When a projection's drift is `invalid` (its source doesn't compile), both `gaffer diff --json` and `gaffer status --json` carry an `error` field with the compile error; `gaffer diff --json` also omits `localHash`. In `gaffer deploy --json`, an `updated` item carries `logic_change: true` when the query changed and deploy continued from the existing checkpoint, so CI can alert on it.
- **`--debug`**: starts the DAP debug server alongside `gaffer dev`. See [Debugging projections](../getting-started/debugging.md).
- **`--env <name>`**: select an environment from `gaffer.toml` for a command that touches a live KurrentDB (`gaffer dev`, `gaffer diff`, `gaffer status`, `gaffer deploy`). Optional when one environment is marked `default`, or on a terminal where you're prompted to pick; required for non-interactive runs with no default.
- **`--connection`**: ad-hoc connection string for a single invocation of `gaffer dev`, `gaffer diff`, `gaffer status`, or `gaffer deploy`. Overrides `--env` and the configured environment.
- **`--fixture <name>`** / **`--events <path>`**: pick a named fixture from `gaffer.toml`, or point at a JSON events file directly. These offline sources are mutually exclusive with the live ones (`--env` / `--connection`); combining the two is a usage error.
- **`--yes` / `-y`**: skip interactive prompts and accept defaults. Applies to `gaffer scaffold`, `gaffer dev`, and `gaffer deploy`. For `gaffer deploy` it stands in as the confirmation, so pass it in scripts and CI: without a terminal, deploy refuses to apply a change unless `--yes` is given. See [Interactive mode](#interactive-mode).
- **`GAFFER_TIMEOUT_MS`** (environment variable): bounds how long a projection may run locally before gaffer treats it as hung, in milliseconds, applied to `gaffer dev` and `gaffer test`. Raise it from the 5000ms default only on slow hardware. The [`[database_config]`](../reference/gaffer-toml.md#database_config) timeouts declare the server's configuration and do not affect local runs.

## Telemetry

The CLI emits anonymous usage telemetry by default. See the [telemetry notice](../telemetry/cli.md) for the full list of what's collected.

Opt out at the user level via any of:

- `gaffer config telemetry off` (persists to the user config file).
- `GAFFER_TELEMETRY_OPTOUT=1` in the environment.
- `KURRENTDB_TELEMETRY_OPTOUT=1` in the environment.
- `DO_NOT_TRACK=1` in the environment.
- VS Code's `telemetry.telemetryLevel` set to `off` (the extension and CLI both respect it).

The environment-variable opt-outs are read from your shell or a project [`.env`](#environment-file-env).

Opt out at the project level by setting `telemetry = false` in [`gaffer.toml`](../reference/gaffer-toml.md#telemetry).
