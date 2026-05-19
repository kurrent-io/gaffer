---
title: MCP
description: Connect gaffer's MCP server to Claude Code, Cursor, Continue, Claude Desktop, VS Code, and other MCP-aware tools.
order: 4
---

`gaffer mcp` is a [Model Context Protocol](https://modelcontextprotocol.io) server that exposes gaffer's projection lifecycle and debugging surface to any MCP-aware AI assistant.

## What's exposed

**Tools** for the projection lifecycle:

- `scaffold` - create a new projection in the project
- `validate` - check a projection for compile errors and runtime gotchas
- `run` / `stop` - run a projection against a fixture or live stream
- `get_state` / `get_step` / `get_history` / `get_timeline` - inspect projection state at any point
- `list_projections` / `list_events` - workspace navigation
- `debug_continue` / `debug_step` / `evaluate` - drive the DAP debugger from natural language

**Resources** for the projection API:

- The full projection API reference
- Worked examples (counters, partitioned state, emit, biState)
- Common gotchas (handler return values, partitioning, system handlers)
- V1 vs V2 engine differences
- Known engine bugs by KurrentDB version

**Prompts** for the two most common workflows:

- `write-projection` - draft a projection from a natural-language description
- `fix-projection` - diagnose and rewrite a broken projection

## Connect your client

`gaffer mcp` is a local stdio server. No auth, no remote endpoint - your MCP client launches it as a subprocess and talks to it over stdin/stdout.

The generic connection shape is `command: gaffer, args: [mcp]`. Client-specific configs:

### VS Code

The [KurrentDB Projections](https://marketplace.visualstudio.com/items?itemName=kurrent-io.gaffer) extension auto-registers gaffer's MCP server with VS Code's MCP framework. Install the extension and Copilot Chat (or any MCP-aware VS Code client) picks it up.

### Claude Code

```sh
claude mcp add gaffer gaffer mcp
```

Then `/mcp` inside Claude Code to confirm gaffer is connected.

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
- Args: `["mcp"]`
- Working directory: a gaffer project (the directory with `gaffer.toml`)

Most clients accept a JSON entry shaped like Cursor's above; consult your client's MCP setup docs.

## Example prompts

Once connected, you can drive gaffer from natural language:

- "Scaffold a projection that counts OrderPlaced events per stream."
- "Run order-count against the happy fixture and show me the final state."
- "Update order-count to also track OrderShipped events."
- "Why isn't my projection handling OrderShipped events?"
- "Set a breakpoint on the OrderPlaced handler, step through the next event, and tell me what the state looks like."

The `write-projection` and `fix-projection` prompts wrap the most common flows. Most clients surface registered prompts as slash commands or a prompt picker.

## Telemetry

The MCP server emits anonymous usage telemetry under the same opt-out cascade as the CLI. See the [telemetry notice](https://telemetry.gaffer.kurrent.io/) for what's collected, or run `gaffer config telemetry off` to disable.
