---
title: VS Code
description: Install the KurrentDB Gaffer VS Code extension for inline run/debug, breakpoints, state inspection, and projection-API autocomplete.
---

The **KurrentDB Gaffer** extension wires gaffer's debugger, language server, MCP server, and tsserver plugin into VS Code. Run and debug projections from `gaffer.toml`, step through handlers, inspect state as it evolves, and get type-aware autocomplete for projection builtins.

## Install

Install the extension from the [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=kurrent-io.gaffer-vscode) or [Open VSX](https://open-vsx.org/extension/kurrent-io/gaffer-vscode).

The extension needs the `gaffer` CLI on PATH. See [Install gaffer](../getting-started/install.md#install-the-cli). If the CLI isn't installed, the extension surfaces a status bar prompt that can run the install for you. If you've customised `gaffer.command` and it points at a binary that no longer exists, a separate prompt offers to open the setting or reset it to the default.

## Bootstrap a project

Run **Gaffer: Scaffold** from the command palette to add a projection. The wizard prompts for a path, event source, partition mode, whether to seed an `emit` example, and the engine version. If the folder has no `gaffer.toml`, the extension runs `gaffer init` first and notes that it did.

Right-clicking a folder in the explorer and picking **Scaffold Projection Here** drops the new file into that folder with a simpler one-step prompt for the filename.

If you only want the `gaffer.toml` without scaffolding a projection, **Gaffer: Init** runs `gaffer init` on its own.

## Run and debug projections

Once `gaffer.toml` exists, the extension adds CodeLenses above each projection block. **Debug** runs live against your default environment, or your only one, and is hidden when there's no single obvious target. **Debug from...** opens a picker of the projection's fixtures and configured environments, so you can debug against any of them.

Set breakpoints in the projection JS file. Standard VS Code debug controls work: step over, into, out, continue. The call stack and scopes views populate with the projection's JS frames and variables.

`Gaffer: Debug` is also available from the command palette: it lists every projection in the workspace, then prompts for a source (a fixture or a configured environment) when there's more than one.

## Authentication

When you debug against an environment that uses [OAuth](../reference/gaffer-toml.md#envnameoauth) and you haven't signed in yet, the run stops and the extension shows a **Sign in** action. It opens a terminal running `gaffer auth --env <name>`. You sign in through the browser once, and the stored token refreshes automatically for later runs.

The token is kept in your OS keyring where one is available (macOS Keychain, Windows Credential Manager, Linux Secret Service), so nothing prompts for a passphrase. On a host without an OS keyring (a remote or container session), the extension manages an encrypted-file keyring for you. It generates a passphrase, stores it in VS Code's secret storage, and passes it to gaffer, so sign-in and later runs work without prompting. To use the CLI directly on such a host, set `GAFFER_KEYRING_PASSWORD` yourself.

## State inspection

A dedicated **Gaffer** panel opens at the bottom of the editor when a session starts. It has three views, two visible at any moment:

- **Status**: connection phase (connecting / catching-up / caught-up / disconnected), total events processed, a count of distinct runtime quirks seen, and a skipped-by-reason rollup. Hidden while paused at a breakpoint. If a run drops on a connection failure, the reason appears in this panel and as a notification.
- **Step**: the event that triggered the current pause, plus a diff of state before and after the handler ran. It also lists what the handler produced as it ran: logs, emitted events, and any runtime quirks that fired. Visible only while paused.
- **State**: current `state`, then `result` (V1 transformed state, V2 post-handler state), then shared state (for biState projections), then per-partition slices. Always visible.

## Autocomplete for projection builtins

A bundled TypeScript server plugin injects projection-runtime types into any `.js` file that shares a workspace root with a registered projection. You get autocomplete and inline docs for `fromAll`, `fromStream`, `fromCategory`, `when`, `emit`, `linkTo`, `partitionBy`, `foreachStream`, and the rest of the API.

The plugin doesn't add a `.d.ts` to your project. Types apply at the tsserver-project level. Disable via `gaffer.injectProjectionTypes` if you don't want it touching loose JS files in projection workspaces.

## MCP integration

The extension auto-registers gaffer's MCP server with VS Code's MCP framework. AI assistants that consume VS Code's MCP providers (GitHub Copilot Chat, others) pick up gaffer's scaffolding / validation / debugging tools without any manual config.

See [MCP](../cli/mcp.md) for the tools and resources gaffer exposes, and for connecting non-VS-Code clients.

## Configuration

| Setting                         | Default      | What it does                                                                                                             |
| ------------------------------- | ------------ | ------------------------------------------------------------------------------------------------------------------------ |
| `gaffer.command`                | `["gaffer"]` | Argv used to invoke gaffer. User scope only. Workspace settings are ignored as defence against hostile workspaces.       |
| `gaffer.debugPort`              | `-1` (auto)  | DAP server port (loopback only). `-1` lets the OS pick a free port and the editor reads it back from the CLI on connect. |
| `gaffer.injectProjectionTypes`  | `true`       | Inject projection-runtime types via the tsserver plugin. Disable to keep non-projection JS clean.                        |
| `gaffer.cliUpdateNotifications` | `true`       | Surface a status bar prompt when a newer gaffer CLI is on npm. The **Never ask** option on the prompt flips this to false. |

## Commands

| Command                      | Invoked via                      | What it does                                                                                                                             |
| ---------------------------- | -------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| `Gaffer: Init`               | Command palette                  | Bootstrap a gaffer project in the current workspace folder (runs `gaffer init`).                                                         |
| `Gaffer: Scaffold`           | Command palette                  | Add a new projection. Prompts for path, source, partition, an emit example, and engine version. Runs `gaffer init` first if no `gaffer.toml` is present. |
| `Scaffold Projection Here`   | Explorer right-click on a folder | Same wizard as Scaffold, but the new file lands in the clicked folder and prompts only for the file name.                                |
| `Gaffer: Debug`              | CodeLens or command palette      | Launch the projection with the debugger attached. Lens uses the projection at the cursor; palette prompts for a projection and source.    |
| `Gaffer: Debug from...`      | CodeLens                         | Pick a source (a fixture or a configured environment) and launch with the debugger attached.                                            |
| `Gaffer: Stop`               | CodeLens or command palette      | Stop the running session.                                                                                                                |

## Telemetry

The extension collects anonymous usage telemetry by default and respects VS Code's `telemetry.telemetryLevel` setting. The first-run notice on the first activation has a **Disable** button that opts out permanently for that install. See the [telemetry notice](../telemetry/vs-code.md) for what's collected.
