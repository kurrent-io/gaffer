<img src="https://raw.githubusercontent.com/kurrent-io/gaffer/main/editors/vscode/media/banner.png" alt="KurrentDB Gaffer" width="100%">

Projection debugger and CodeLens companion for [KurrentDB](https://www.kurrent.io). Run and debug projections from `gaffer.toml`, step through handlers with full state inspection, and get type-aware autocomplete for projection builtins.

Powered by the [gaffer](https://www.npmjs.com/package/@kurrent/gaffer) toolkit.

## Features

**Debug projections from `gaffer.toml`.** CodeLens above each projection and fixture block. Click Debug to launch a session locally. Set breakpoints to pause for inspection, or let it run through.

**Step through handlers.** Set breakpoints in your projection JS. Step over, into, and out of handlers. Watch state evolve event by event in the dedicated panel.

**Live state inspection.** The Gaffer panel shows the current step, partitioned state, shared state (for biState projections), emitted events, and console output as the projection runs.

**Type-aware autocomplete for projection builtins.** A TypeScript server plugin injects `fromStream`, `when`, `emit`, `linkTo`, `partitionBy`, and the rest of the projection API into JavaScript files that share a workspace root with a registered projection. No imports needed.

**MCP server auto-registration.** The gaffer MCP server (scaffolding, validation, debugging tools, projection API resources) is auto-registered with VS Code's MCP framework. Available to GitHub Copilot, Claude, and any other MCP-aware tooling.

## Quick start

1. Install the extension.
2. Open a folder and add projections via `Gaffer: Scaffold` (palette) or right-click a folder and pick `Scaffold Projection Here`. A fresh folder gets a `gaffer.toml` created for it automatically as part of the first scaffold; run `Gaffer: Init` from the palette if you'd rather create the toml without scaffolding.
3. Click Debug above any projection in `gaffer.toml`, or run `Gaffer: Debug` from the command palette.

The extension spawns the [`@kurrent/gaffer` CLI](https://www.npmjs.com/package/@kurrent/gaffer) for LSP, MCP, and debug sessions. It will offer to install the CLI on first use if it isn't already on `PATH`. Requires `@kurrent/gaffer` 0.1.0 or later.

## Configuration

| Setting                        | Default      | What it does                                                                                                       |
| ------------------------------ | ------------ | ------------------------------------------------------------------------------------------------------------------ |
| `gaffer.command`               | `["gaffer"]` | Argv used to invoke gaffer. User scope only; workspace settings are ignored as defense against hostile workspaces. |
| `gaffer.debugPort`             | `-1` (auto)  | DAP server port (loopback only). `-1` lets the OS pick a free port and the editor reads it back on connect.        |
| `gaffer.injectProjectionTypes` | `true`       | Inject projection-runtime types via the tsserver plugin. Disable to keep non-projection JS clean.                  |

## Commands

| Command                      | What it does                                                                                              |
| ---------------------------- | --------------------------------------------------------------------------------------------------------- |
| `Gaffer: Init`               | Create a `gaffer.toml` in the current folder                                                              |
| `Gaffer: Scaffold`           | Add a new projection to the project via a multi-step picker. Right-clicking a folder runs it scoped there |
| `Gaffer: Debug`              | Pick a projection from the workspace and launch a debug session                                           |
| `Gaffer: Debug from Fixture` | Codelens-triggered Debug session bound to a specific fixture in `gaffer.toml`                             |
| `Gaffer: Stop`               | Stop the running session                                                                                  |

## Telemetry

The extension collects anonymous usage telemetry by default and respects VS Code's `telemetry.telemetryLevel` setting. See [TELEMETRY.md](TELEMETRY.md) for the full list of what is collected and how to opt out.

## Documentation

Full documentation at <https://gaffer.kurrent.io/>.

Bugs go to [GitHub Issues](https://github.com/kurrent-io/gaffer/issues). Questions and feature requests to [Discussions](https://github.com/kurrent-io/gaffer/discussions).

## License

[Kurrent License v1](LICENSE)
