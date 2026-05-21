---
title: Commands
description: Full reference for every gaffer subcommand and its flags.
---

Full reference for every gaffer subcommand. Generated from the CLI source; run `just gen-docs` to refresh after touching a command.

## gaffer init

Initialize a new gaffer project.

Creates gaffer.toml in the current directory.

```
gaffer init [flags]
```

Flags:

```
  -y, --yes   Accept all defaults, no prompts (currently the only mode)
```

## gaffer scaffold

Add a new projection to the project.

Create a projection at <path>. The path is resolved relative to the current directory and must end in a supported extension (.js). The projection's gaffer.toml key defaults to the file's basename; pass --name to override.

```
gaffer scaffold <path> [flags]
```

Flags:

```
      --emit               Enable emit/linkTo
      --name string        Projection name in gaffer.toml (defaults to the file's basename)
      --partition string   Partitioning (none, per-stream) (default "none")
      --source string      Event source (all, stream:name, category:name) (default "all")
```

## gaffer dev

Run a projection locally.

```
gaffer dev [projection] [flags]
```

Flags:

```
      --connection string                KurrentDB connection string (overrides config)
      --debug                            Start DAP debug server
      --debug-port int                   DAP debug server port (0 = OS picks a free port; the actual bound port is reported on stderr and in --json output)
      --events string                    Path to a JSON events file (ad-hoc fixture)
      --fixture string                   Named fixture declared as fixtures.<name> in gaffer.toml
      --json                             Output as NDJSON
      --start-paused-if-no-breakpoints   Pause at the start of the first event when no breakpoints are set (debug mode only)
      --until-caught-up                  Exit when subscription catches up (live mode only)
```

## gaffer info

Show projection details.

```
gaffer info [projection] [flags]
```

Flags:

```
      --json   Output as JSON
```

## gaffer mcp

Start an MCP server for AI agent integration.

```
gaffer mcp
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
default. See https://telemetry.gaffer.kurrent.io (and `gaffer config
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

