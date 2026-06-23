---
title: Commands
description: Full reference for every gaffer subcommand and its flags.
---

Full reference for every gaffer subcommand. Generated from the CLI source; run `just gen-docs` to refresh after touching a command.

## gaffer init

Initialize a new gaffer project.

Creates a starter gaffer.toml in the current directory. Define an environment and add projections with `gaffer scaffold`.

```
gaffer init
```

## gaffer scaffold

Add a new projection to the project.

Create a projection at <path>. The path is resolved relative to the current directory and must end in a supported extension (.js). The projection's gaffer.toml key defaults to the file's basename; pass --name to override. Run without <path> on a terminal to be prompted for the path and options.

```
gaffer scaffold <path> [flags]
```

Flags:

```
      --emit                 Enable emit/linkTo
      --engine-version int   Projection engine version (1 or 2) (default 2)
      --name string          Projection name in gaffer.toml (defaults to the file's basename)
      --partition string     Partitioning (none, per-stream) (default "none")
      --source string        Event source (all, stream:name, category:name) (default "all")
  -y, --yes                  Skip prompts (a path must be supplied without prompting)
```

## gaffer dev

Run a projection locally.

Run a projection locally against a fixture or live KurrentDB. Run without <projection> on a terminal to pick one, and to pick a source when none is given via --events / --fixture / --connection.

```
gaffer dev <projection> [flags]
```

Flags:

```
      --connection string                KurrentDB connection string (overrides --env and config)
      --debug                            Start DAP debug server
      --debug-port int                   DAP debug server port (0 = OS picks a free port; the actual bound port is reported on stderr and in --json output)
      --env string                       Environment to run against, from gaffer.toml [env.<name>] (defaults to the env marked default)
      --events string                    Path to a JSON events file (ad-hoc fixture)
      --fixture string                   Named fixture declared as fixtures.<name> in gaffer.toml
      --json                             Output as NDJSON
      --start-paused-if-no-breakpoints   Pause at the start of the first event when no breakpoints are set (debug mode only)
      --until-caught-up                  Exit when subscription catches up (live mode only)
  -y, --yes                              Skip prompts (a projection and source must be resolvable without prompting)
```

## gaffer info

Show projection details.

```
gaffer info <projection> [flags]
```

Flags:

```
      --json   Output as JSON
```

## gaffer auth

Authenticate to an environment's OAuth identity provider.

Signs in to the environment's OAuth identity provider with an interactive browser
login (authorization code + PKCE) and stores the resulting token, which gaffer
refreshes automatically. It applies to environments configured for OAuth in
gaffer.toml. For CI, set KURRENTDB_OAUTH_CLIENT_SECRET instead to use the
non-interactive client-credentials grant.

--clear removes every stored token, signing out of all environments. Use it to
reset a keyring whose passphrase has been forgotten; it needs neither the
passphrase nor a gaffer project.

GAFFER_NO_OPEN prints the authorization URL instead of opening a browser.
GAFFER_KEYRING_PASSWORD supplies the keyring passphrase on a host without an OS keyring.

```
gaffer auth [flags]
```

Flags:

```
      --clear        Remove every stored token, signing out of all environments
      --env string   Environment to authenticate (defaults to the env marked default)
```

## gaffer mcp

Start an MCP server for AI agent integration.

```
gaffer mcp [flags]
```

Flags:

```
      --project string   Project directory to use instead of searching from the working directory (also set via GAFFER_PROJECT)
```

## gaffer lsp

Run the gaffer LSP server over stdio.

Run the gaffer Language Server Protocol server, speaking JSON-RPC over stdin/stdout. Editor extensions spawn this subcommand and connect to it as an LSP client.

```
gaffer lsp
```

## gaffer config

Manage gaffer's user configuration.

Read or change gaffer's user-level settings.

Settings live at $XDG_CONFIG_HOME/gaffer/config.toml (on macOS,
~/Library/Application Support/gaffer/config.toml; on Windows,
%AppData%/gaffer/config.toml). The GAFFER_CONFIG_DIR environment
variable overrides the default location.

## gaffer config telemetry

Show or change telemetry settings.

Telemetry is anonymous usage data gaffer sends to Kurrent so we can
understand which features people use. It is opt-out: enabled by
default. See https://gaffer.kurrent.io/telemetry/ (and `gaffer config
telemetry status`) for exactly what is collected and how to turn it off.

## gaffer config telemetry off

Disable telemetry on this machine.

Set the user-level telemetry preference to disabled and clear the
per-install id and salt. Prints the cleared id one last time so you
can capture it for a deletion request (email privacy@kurrent.io).

```
gaffer config telemetry off
```

## gaffer config telemetry on

Enable telemetry on this machine.

Set the user-level telemetry preference to enabled.

If telemetry isn't already in active use, this mints a fresh per-
install id and prints a one-time disclosure notice. Existing
environment-variable or workspace opt-outs still take precedence;
the command surfaces them so you know what else to change.

```
gaffer config telemetry on
```

## gaffer config telemetry status

Show current telemetry configuration.

Print the current telemetry state, broken down by source. Use this
to find which layer (user config, environment variable, or workspace
gaffer.toml) is enabling or disabling telemetry for this invocation.

Always exits 0.

```
gaffer config telemetry status
```

## gaffer version

Print the gaffer version.

```
gaffer version
```

## gaffer deploy

Create or update projections on an environment.

Deploy projections from gaffer.toml to a KurrentDB environment: create the ones not yet on the server, update the ones whose definition changed, and skip the ones already in sync (matched by content hash).

With no argument, deploys every projection in gaffer.toml; name one to deploy just it. The emit flag is always sent explicitly so an update never clears it.

A changed query is a logic change: the new code may interpret already-processed events differently, so the accumulated state could now be wrong. By default deploy continues from the existing checkpoint (state is kept) and flags the change. Pass --reset-on-logic-change to rebuild instead, reprocessing from zero with the new logic (slower, and an emitting projection re-emits). A change to engine version or track-emitted-streams can't be applied in place; deploy refuses it and points you at gaffer recreate.

Every projection is compiled before anything is sent to the server; if any fails to compile or has errors that would fault on the server, the whole deploy is refused so a bad projection can't leave a half-applied set. --no-validate skips this check.

When the plan would change something, deploy shows it and asks to confirm before applying; updating a projection that's currently faulted is flagged, since the update won't clear the fault. --yes skips the prompt; without a terminal (or with --json) deploy won't apply unconfirmed, so pass --yes in scripts. A server that reports itself as production gets a louder confirm and refuses --no-validate. Pass --json for machine-readable output.

```
gaffer deploy [projection] [flags]
```

Flags:

```
      --connection string       KurrentDB connection string (overrides --env)
      --env string              Environment from gaffer.toml to deploy to
      --json                    Output as JSON
      --no-validate             Skip the preflight compile check and deploy anyway
      --reset-on-logic-change   Rebuild from zero on a logic change instead of continuing from checkpoint
  -y, --yes                     Skip the confirmation prompt
```

## gaffer diff

Show how a projection differs from what's deployed.

Compare a projection's local definition against what's deployed on KurrentDB.

Reports one of five states: in sync, drifted, not deployed (local only), untracked (on the server but absent from gaffer.toml), or invalid. Invalid means the local source doesn't compile; the source and config still diff, but emit is unknown.

When the query differs, the source is shown in an external diff viewer (git diff --no-index by default; set GAFFER_EXTERNAL_DIFF to override). Pass --json for machine-readable output.

```
gaffer diff <projection> [flags]
```

Flags:

```
      --connection string   KurrentDB connection string (overrides --env)
      --env string          Environment from gaffer.toml to compare against
      --json                Output as JSON
```

## gaffer status

Show the state of projections on an environment.

Show the runtime state of projections on a KurrentDB environment and how they
compare to local config.

With no argument, lists every local and deployed projection: running, stopped or
faulted, progress, and whether each is in sync, drifted, not deployed, untracked,
or invalid (local source doesn't compile). Name a projection for its detail. Pass
--json for machine output.

```
gaffer status [projection] [flags]
```

Flags:

```
      --connection string   KurrentDB connection string (overrides --env)
      --env string          Environment from gaffer.toml
      --json                Output as JSON
```

