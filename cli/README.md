<img src="../assets/banner-gaffer.svg" alt="Gaffer" width="100%">

Develop, test, debug, and deploy [KurrentDB](https://kurrent.io) projections.

KurrentDB projections are server-side JavaScript that derive new streams and state from existing events. Gaffer runs them locally against the same JavaScript engine KurrentDB uses, so a projection that passes here passes in production.

## Install

```sh
npm install -g @kurrent/gaffer
```

Requires Node.js 22 or later. Platform binaries are pulled in as optional dependencies; no separate runtime install is needed.

## Quick start

Try the bundled demo:

```sh
git clone https://github.com/kurrent-io/gaffer
cd gaffer/demo
gaffer dev order-count --fixture happy
```

The CLI replays the events declared as `fixtures.happy` in [`demo/gaffer.toml`](https://github.com/kurrent-io/gaffer/tree/main/demo) through the `order-count` projection and prints the resulting state. No running KurrentDB instance is needed.

![gaffer dev replaying the order-count projection against the happy fixture](../assets/demo-dev.gif)

Or start a new project from scratch:

```sh
gaffer init                            # create gaffer.toml
gaffer scaffold projections/order.js   # add a projection at that path
```

See the [getting started guide](https://gaffer.kurrent.io/getting-started/install/) for a full walkthrough including fixtures, editor setup, and the dev loop.

## Commands

Develop locally:

| Command                    | What it does                                                            |
| -------------------------- | ------------------------------------------------------------------------ |
| `gaffer init`              | Create `gaffer.toml` in the current directory                            |
| `gaffer scaffold <path>`   | Add a new projection at `<path>` (must end in `.js`)                     |
| `gaffer dev <projection>`  | Run a projection against fixtures, an events file, or a live KurrentDB   |
| `gaffer info <projection>` | Show projection details                                                  |

Deploy & operate:

| Command                        | What it does                                                        |
| ------------------------------ | -------------------------------------------------------------------- |
| `gaffer diff <projection>`     | Compare two versions of a projection                                 |
| `gaffer status`                | Show the state of projections on an environment                      |
| `gaffer history <projection>`  | Show a deployed projection's history                                 |
| `gaffer deploy`                | Create or update projections on an environment                       |
| `gaffer rollback <projection> <hash>` | Roll a projection back to a version from its history          |
| `gaffer enable <projection>`   | Enable (start) a projection on an environment                        |
| `gaffer disable <projection>`  | Disable (stop) a projection on an environment                        |
| `gaffer recreate <projection>` | Destroy and rebuild a projection from local config                   |
| `gaffer delete <projection>`   | Delete a projection from an environment                              |

Tools & config:

| Command          | What it does                                                |
| ---------------- | ------------------------------------------------------------ |
| `gaffer auth`    | Authenticate to an environment's OAuth identity provider     |
| `gaffer config`  | Manage user-level configuration (telemetry, identity)        |
| `gaffer mcp`     | Start an MCP server for AI agent integration                 |
| `gaffer lsp`     | Run the Language Server Protocol server over stdio           |
| `gaffer version` | Print the gaffer version                                     |

Run `gaffer <command> --help` for flags and options, or see the [full command reference](https://gaffer.kurrent.io/cli/).

## Configuration

Project settings live in `gaffer.toml` at the project root:

```toml
[env.local]
connection = "kurrentdb://localhost:2113?tls=false"
default = true

[[projection]]
name = "order-count"
entry = "projections/order-count.js"
engine_version = 2
fixtures.happy = "fixtures/orders.json"
```

Each `[env.<name>]` block names a deploy target; credentials come from environment variables or `.env` / `.env.<name>` files next to the config. User-level settings (telemetry opt-out, identity) live in `~/.config/gaffer/config.toml` and are managed with `gaffer config`. See the [configuration reference](https://gaffer.kurrent.io/reference/gaffer-toml/) for every option.

## Editor integration

Gaffer ships with Language Server Protocol and Debug Adapter Protocol servers, plus a VS Code extension that wires them up automatically.

- **VS Code** - install the [KurrentDB Gaffer extension](https://gaffer.kurrent.io/extension/vs-code/) for inline diagnostics, run/debug codelens above `gaffer.toml` projections, breakpoint debugging, and per-projection deploy status, with deploy, history, and rollback actions inline.
- **Other editors** - run `gaffer lsp` over stdio for LSP integration. See the [other editors guide](https://gaffer.kurrent.io/extension/other-editors/) for examples.

## AI agent integration

`gaffer mcp` exposes scaffolding, validation, debugging, deploy and operate verbs, and the projection API as Model Context Protocol tools and resources. Compatible with Claude Code, Cursor, Continue, GitHub Copilot, and any other MCP client.

See the [MCP integration guide](https://gaffer.kurrent.io/cli/mcp/) for client setup.

## Related packages

| Package                                                                                                    | What it is                                                                           |
| ---------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| [`@kurrent/projections-testing`](https://www.npmjs.com/package/@kurrent/projections-testing)               | Library for testing projections from your existing test runner (vitest, jest, mocha) |
| [KurrentDB Gaffer for VS Code](https://gaffer.kurrent.io/extension/vs-code/)                               | Editor integration with debugger, codelens, and MCP server                           |

## Telemetry

Gaffer collects anonymous usage telemetry by default. See [TELEMETRY.md](https://github.com/kurrent-io/gaffer/blob/main/cli/TELEMETRY.md) for the full list of what is collected and how to opt out (`gaffer config telemetry off`).

## Documentation

Full documentation at <https://gaffer.kurrent.io/>.

Bugs go to [GitHub Issues](https://github.com/kurrent-io/gaffer/issues). Questions and feature requests to [Discussions](https://github.com/kurrent-io/gaffer/discussions).

## License

[Kurrent License v1](LICENSE)
