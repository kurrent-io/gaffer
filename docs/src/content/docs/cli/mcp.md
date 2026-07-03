---
title: MCP
description: Connect gaffer's MCP server to Claude Code, Cursor, Continue, Claude Desktop, VS Code, and other MCP-aware tools.
---

`gaffer mcp` is a [Model Context Protocol](https://modelcontextprotocol.io) server that exposes gaffer's projection lifecycle and debugging surface to any MCP-aware AI assistant.

## What's exposed

**Tools** for the projection lifecycle:

- **`init`**: create a `gaffer.toml` to start a new project when there isn't one yet.
- **`scaffold`**: create a new projection at an explicit path and register it in `gaffer.toml`. Accepts an `engine_version` argument (`1` or `2`, defaults to `2`).
- **`validate`**: check a projection for compile errors and runtime gotchas. Reports `valid: false` for a source that fails to compile or carries an error-severity diagnostic (a feature the server rejects, such as `track_emitted_streams` on `engine_version 2`), with the diagnostic in `lastError`.
- **`run`** / **`stop`**: run a projection against a fixture or live stream, and stop a running session. A live `run` accepts an `env` argument to choose the environment; omit it for the default.
- **`get_state`** / **`get_step`** / **`get_history`** / **`get_timeline`**: inspect projection state at any point. `get_step`, `get_history`, and `get_timeline` also surface the runtime quirks that fired on each step, so the assistant can spot one and cross-reference its code against the `gaffer://docs/quirks` resource.
- **`get_projection_info`**: return a projection's parsed structure, including sources, partition mode, whether it emits events, and effective engine version.
- **`list_projections`** / **`list_events`**: workspace navigation. `list_events` samples from a live connection and accepts an `env` argument to choose the environment.
- **`debug_continue`** / **`debug_step_over`** / **`debug_step_into`** / **`debug_step_out`** / **`evaluate`**: drive the DAP debugger from natural language.
- **`deploy_status`** / **`deploy_plan`** / **`deploy_history`**: read-only visibility into deployed environments, mirroring `gaffer status --json`, `gaffer deploy --dry-run --json`, and `gaffer history --json`. `deploy_status` shows each projection's runtime state and drift verdict (plus any `[database_config]` divergence, as [`gaffer status`](./commands.md#gaffer-status) warns); `deploy_plan` previews what a deploy would change without applying anything; `deploy_history` reads a projection's per-deploy audit log, pageable with `before`. All three accept an `env` argument; omit it for the default environment. The deploy *action* and the operate verbs have no MCP surface - applying changes stays in the CLI, behind its confirmation gates.
- **`get_version`**: report the gaffer CLI version backing the server.

**Resources**:

- The full projection API reference.
- Worked examples covering counters, partitioned state, `emit`, biState, and the rest.
- Common gotchas across the projection API.
- V1 vs V2 engine differences.
- Known engine quirks by KurrentDB version.
- The current `gaffer.toml`, exposed so the assistant can reason about projection registration.
- Telemetry disclosure, so the assistant can answer questions about what gaffer collects.

**Prompts** for the two most common workflows:

- **`write-projection`**: draft a projection from a natural-language description.
- **`fix-projection`**: diagnose and rewrite a broken projection.

## Connect your client

`gaffer mcp` is a local stdio server. No auth, no remote endpoint - your MCP client launches it as a subprocess and talks to it over stdin/stdout.

The server starts whether or not its working directory is a gaffer project, so it is safe to register globally. Without a project, the documentation resources and `get_version` are available immediately, and the projection tools return an error pointing you at the `init` tool. They start working as soon as a `gaffer.toml` exists, whether created by `init`, created by `gaffer init`, or already present in the working directory or a parent. No restart needed.

When the launch directory is not your project (common for a globally registered server), pass `--project <dir>` or set `GAFFER_PROJECT`. The flag takes precedence over the variable, and both override the working-directory search.

The generic connection shape is `command: gaffer, args: [mcp]`. Client-specific configs:

### VS Code

The [KurrentDB Gaffer](../extension/vs-code.md) extension auto-registers gaffer's MCP server with VS Code's MCP framework. Install the extension and Copilot Chat (or any MCP-aware VS Code client) picks it up.

### Claude Code

```sh
claude mcp add gaffer -- gaffer mcp
```

Then `/mcp` inside Claude Code to confirm gaffer is connected. When registering from outside your project, pin it with `claude mcp add gaffer -- gaffer mcp --project /path/to/project`.

### Cursor

Add to `.cursor/mcp.json` in your workspace (or `~/.cursor/mcp.json` for global):

```json
{
  "mcpServers": {
    "gaffer": {
      "command": "gaffer",
      "args": ["mcp"]
    }
  }
}
```

### Claude Desktop

Edit the config file (platform-specific):

- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Windows: `%AppData%\Claude\claude_desktop_config.json`

Add:

```json
{
  "mcpServers": {
    "gaffer": {
      "command": "gaffer",
      "args": ["mcp"]
    }
  }
}
```

Restart Claude Desktop. Look for the slider icon in the chat input to confirm tools are connected.

### Other MCP clients

Any MCP-aware client that supports stdio servers works. Point it at:

- Command: `gaffer`
- Args: `["mcp"]` (add `["mcp", "--project", "/path/to/project"]`, or set `GAFFER_PROJECT`, when the launch directory is not your project)
- Working directory: any directory. The projection tools need a `gaffer.toml` in the working directory or a parent, or a `--project` / `GAFFER_PROJECT` override; the documentation resources and `get_version` work anywhere.

Most clients accept a JSON entry shaped like Cursor's above. Consult your client's MCP setup docs.

## Example prompts

Once connected, you can drive gaffer from natural language:

- "Scaffold a projection that counts OrderPlaced events per stream."
- "Run order-count against the happy fixture and show me the final state."
- "Update order-count to also track OrderShipped events."
- "Why isn't my projection handling OrderShipped events?"
- "Set a breakpoint on the OrderPlaced handler, step through the next event, and tell me what the state looks like."

The `write-projection` and `fix-projection` prompts wrap the most common flows. Most clients surface registered prompts as slash commands or a prompt picker.

## Telemetry

The MCP server emits anonymous usage telemetry under the same opt-out cascade as the CLI. See the [telemetry notice](../telemetry/cli.md) for what's collected, or run `gaffer config telemetry off` to disable.
