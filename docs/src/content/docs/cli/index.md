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
| [`gaffer diff <projection>`](./commands.md#gaffer-diff) | Compare two versions of a projection. By default, local against deployed (in sync, drifted, not deployed, untracked, or invalid); `--left`/`--right` compare any two versions (`local`, `deployed`, or a history content-hash). |
| [`gaffer status [projection]`](./commands.md#gaffer-status) | Show the runtime state of projections on an environment and how they compare to local config. |
| [`gaffer history <projection>`](./commands.md#gaffer-history) | Show a deployed projection's version history, newest first, attributing each change (deploy, updated, lifecycle) with its deployer and a content hash. Interactive on a terminal; `--json`/piped prints the latest versions. |
| [`gaffer deploy [projection]`](./commands.md#gaffer-deploy) | Create or update projections on an environment: create the new ones, update the changed ones, skip the in-sync ones. Plans the whole run, then validates it and refuses if any projection won't run (`--no-validate` bypasses). Shows the plan and confirms before applying (`--yes` skips); a production target (server-declared, or `production = true` on the env) is extra-guarded and refuses `--no-validate`. `--dry-run` shows the plan and applies nothing. Exits with a [stable code](#exit-codes) for CI. |
| [`gaffer rollback <projection> <hash>`](./commands.md#gaffer-rollback) | Roll a deployed projection back to a prior version from its history, named by content hash. Confirms with a diff first; local files stay untouched. |
| [`gaffer enable <projection>`](./commands.md#gaffer-enable) | Enable (start) a deployed projection so it resumes from its last checkpoint. |
| [`gaffer disable <projection>`](./commands.md#gaffer-disable) | Disable (stop) a deployed projection, writing a final checkpoint (`--abort` skips it). |
| [`gaffer recreate <projection>`](./commands.md#gaffer-recreate) | Destroy and rebuild a deployed projection from local config, reprocessing from zero (`--delete-emitted` also wipes emitted streams). |
| [`gaffer delete <projection>`](./commands.md#gaffer-delete) | Delete a deployed projection with its state and checkpoint streams (`--delete-emitted` also removes emitted streams). |
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

- **`[env.<name>]`**: an environment, naming a KurrentDB connection. Each block has a required **`connection`** (the connection string, supporting `${VAR}` expansion so credentials can stay out of the file) and an optional **`default`** bool. Exactly one environment may be the default. An optional **`production`** bool opts the environment into the production guard tier (louder confirmations, `--no-validate` refused). Select an environment with `gaffer dev --env <name>` or pick it from the interactive prompt; `--env` can be omitted on a non-interactive run when one environment is the default. An environment can also authenticate with OAuth, an X.509 client certificate, or basic credentials; see [Authentication](./authentication.md). See [Environment file](#environment-file-env) and the [gaffer.toml reference](../reference/gaffer-toml.md#envname).

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

- **`--json`**: structured output instead of the default text rendering. `gaffer dev --json` emits NDJSON, one object per line, each tagged with a `type`: `info`, `event`, `result`, `error`, `fatal_error`, `summary`, `auth_required`, and `run_error`. `auth_required` signals that a live run needs an interactive sign-in, and `run_error` that a run ended on a connection failure. `gaffer deploy --json` emits a JSON array, one object per projection; `gaffer deploy --dry-run --json` instead emits an envelope object - a top-level `verdict` (`in-sync`, `deployable`, or `blocked`, what a real deploy would do), the `changes` count, the resolved `env`/`target` and whether it's `production`, any `configDrift`, and the per-projection `plan` array. Adding `--stream` switches `gaffer deploy --json`'s apply to NDJSON, one event per line tagged with a `type`. A `deploy_start` marks each projection beginning (with `index`/`total`), a `deploy_result` carries its settled outcome (the array's per-projection shape), and a terminal `deploy_summary` counts the outcomes. It streams progress live instead of buffering the whole run, and like `gaffer dev --json` it exits non-zero if the stream can't be written. `--stream` is for the apply: it can't be combined with `--dry-run` (a preview stays `--dry-run --json`), and stdout stays strictly one-object-per-line throughout, so even a pre-apply refusal streams the blocking projections and a terminal `deploy_summary`. `gaffer status --json` emits an object: the resolved `env`, `target` server, and whether it's `production`, a `projections` array (one object per projection), plus an optional `configDrift` array. `configDrift` appears when the target's engine config diverges from a declared `[database_config]`, one `{"knob", "server", "local"}` object per differing knob in its native unit; when the node's options can't be read (auth refusal, no HTTP surface), a `configDriftError` string carries the reason instead, so a missing `configDrift` is never mistaken for "in sync". `gaffer history --json` emits a JSON array, one object per entry; other commands emit a single JSON object. `gaffer status --json` carries, per projection, `owner` (`in-config`, `orphan`, `foreign`, or `unknown`); a `hash` (the deployed definition's content hash) when the projection is deployed; a `lastDeployed` timestamp and a `lastWrite` (the deploying tool, its version, the source revision, and the actor) when the projection carries deploy metadata; `attribution` (`local-ahead`, `changed-by-tool`, or `changed-server`) on a drifted projection; and, when its drift is `invalid` (the local definition doesn't compile or has a config error), a `reason` field with the compile or config error. `gaffer diff --json` describes the two versions being compared as `left` and `right` (each with `ref`, content `hash`, and canonical `source`), plus a structured `lines` array (each row tagged `equal`, `removed`, or `added`, with per-side line numbers and the changed intraline span). For the default deployed-vs-local diff it also carries a `verdict` object holding the same `drift`, `owner`, `attribution`, `lastDeployed`, `lastWrite`, and `reason` fields as status; a version-to-version diff (`--left`/`--right` other than the default) is a pure source diff with no verdict. The `gaffer status` table likewise gains `LAST DEPLOY` and `DEPLOYED VIA` columns. In `gaffer deploy --json` (and each `plan` item of `--dry-run --json`), an `updated` item carries `logicChange: true` when the query changed and deploy continued from the existing checkpoint, and `externalChange: true` (with `externalChangeTool` naming the tool, when another made the change) when the deployed definition was changed outside gaffer since its last deploy, so CI can alert on either. A `refused` item carries `recreate: true` (it's valid but needs a recreate), distinct from an `invalid` item whose local definition won't run; an item may also carry `faulted` (an update over a faulted projection) or `emittingReset` (a rebuild that re-emits). Each `gaffer history --json` entry carries `version`, `time`, the full `contentHash`, a `kind` (`deploy`, `rollback`, `reset`, `recreate`, `updated-by`, `updated`, `enabled`, `disabled`, `reconfigured`, `rewritten`, `created`, `deleted`, or `unreadable`), and the `enabled` and `outOfBand` flags (the latter true for a non-gaffer write once gaffer has been managing the projection). A recreate's disable and delete writes stay separate entries in `--json` (the human timeline folds them into the `recreate` entry). The `stateChange` and `deleted` flags appear only when true; the `tool`, `toolVersion`, `actor`, `operation`, and `revision` come with a metadata-carrying entry; and `configChanges` (the checkpoint or performance knobs that moved) comes on a `reconfigured` entry. `gaffer enable`, `gaffer disable`, `gaffer delete`, and `gaffer recreate` each emit `{"name", "outcome"}`, where `outcome` is `enabled`, `disabled`, `aborted`, `deleted`, or `recreated`. `gaffer rollback` emits `{"name", "outcome", "hash"}` with the full target content hash, where `outcome` is `rolled-back`, or `unchanged` when the target is already deployed. If `gaffer dev --json` can't write its stream (for example, a broken pipe to the editor), it exits non-zero instead of finishing silently, so a consumer can tell the NDJSON stream was truncated.
- **`--debug`**: starts the DAP debug server alongside `gaffer dev`. See [Debugging projections](../getting-started/debugging.md).
- **`--env <name>`**: select an environment from `gaffer.toml` for a command that touches a live KurrentDB (`gaffer dev`, `gaffer diff`, `gaffer status`, `gaffer history`, `gaffer deploy`, `gaffer rollback`, `gaffer enable`, `gaffer disable`, `gaffer recreate`, `gaffer delete`). Optional when one environment is marked `default`, or on a terminal where you're prompted to pick; required for non-interactive runs with no default.
- **`--connection`**: ad-hoc connection string for a single invocation of `gaffer dev`, `gaffer diff`, `gaffer status`, `gaffer history`, `gaffer deploy`, `gaffer rollback`, `gaffer enable`, `gaffer disable`, `gaffer recreate`, or `gaffer delete`. Overrides `--env` and the configured environment.
- **`--fixture <name>`** / **`--events <path>`**: pick a named fixture from `gaffer.toml`, or point at a JSON events file directly. These offline sources are mutually exclusive with the live ones (`--env` / `--connection`); combining the two is a usage error.
- **`--yes` / `-y`**: skip interactive prompts and accept defaults. Applies to `gaffer scaffold`, `gaffer dev`, `gaffer deploy`, and the guarded operate verbs (`gaffer delete`, `gaffer recreate`, and `gaffer rollback` always, `gaffer disable` against production). For `gaffer deploy`, `gaffer delete`, `gaffer recreate`, `gaffer rollback`, and a production `gaffer disable` it stands in as the confirmation, so pass it in scripts and CI: without a terminal those refuse to act unless `--yes` is given. See [Interactive mode](#interactive-mode).
- **`GAFFER_ACTOR`** / **`GAFFER_REVISION`** (environment variables, `gaffer deploy`): override the acting identity and source revision recorded in deploy metadata. They default to the connection's user and the project's git commit; set these in CI to record the pipeline identity, or the canonical commit when the checkout's HEAD isn't it (e.g. a PR build's synthetic merge commit).
- **`GAFFER_TIMEOUT_MS`** (environment variable): bounds how long a projection may run locally before gaffer treats it as hung, in milliseconds, applied to `gaffer dev` and `gaffer test`. Raise it from the 5000ms default only on slow hardware. The [`[database_config]`](../reference/gaffer-toml.md#database_config) timeouts declare the server's configuration and do not affect local runs.

## Exit codes

`gaffer deploy` uses a stable exit code so a pipeline can branch on the result:

- **`0`**: succeeded, or nothing to do (everything already in sync).
- **`1`**: an error. A projection is invalid (won't compile, or would fault on the server), a server call failed, or the plan has a projection deploy can't apply in place (a refusal needing a recreate).
- **`2`**: changes are pending. Only `--dry-run` returns this; it means the plan has work to apply.
- **`3`**: refused by a guardrail. Confirmation was needed but there was no terminal and no `--yes`, or `--no-validate` was used against production. Re-run satisfying the guardrail. `gaffer recreate`, `gaffer rollback`, `gaffer disable`, and `gaffer delete` also exit `3` when a guarded action can't confirm non-interactively (`gaffer enable` has no guarded action, so it never does).
- **`4`**: an environment needs an interactive sign-in (no stored token, or a locked keyring). Run `gaffer auth --env <name>`, then retry. Any command that connects to an OAuth environment (`deploy`, `diff`, `status`, `history`, the operate verbs) exits `4` when it has no stored credential. `gaffer diff` also exits `4` when a stored token is rejected mid-command, such as an expired session.

A typical CI check is `gaffer deploy --dry-run`: exit `0` means in sync, `2` means drift to apply, `1` means something needs attention.

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
