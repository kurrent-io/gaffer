# KurrentDB Projections

Projection debugger and CodeLens companion for [KurrentDB](https://www.kurrent.io). Run and debug projections from `gaffer.toml`, step through handlers with full state inspection, and get type-aware autocomplete for projection builtins.

Powered by the [gaffer](https://www.npmjs.com/package/@kurrent/gaffer) toolkit.

## Features

**Run and debug projections from `gaffer.toml`.** CodeLens above each projection and fixture block. Click Run or Debug to launch a session locally without leaving the editor.

**Step through handlers.** Set breakpoints in your projection JS. Step over, into, and out of handlers. Watch state evolve event by event in the dedicated panel.

**Live state inspection.** The KurrentDB Projections panel shows the current step, partitioned state, shared state (for biState projections), emitted events, and console output as the projection runs.

**Type-aware autocomplete for projection builtins.** A TypeScript server plugin injects `fromStream`, `when`, `emit`, `linkTo`, `partitionBy`, and the rest of the projection API into JavaScript files that share a workspace root with a registered projection. No imports needed.

**MCP server auto-registration.** The gaffer MCP server (scaffolding, validation, debugging tools, projection API resources) is auto-registered with VS Code's MCP framework. Available to GitHub Copilot, Claude, and any other MCP-aware tooling.

## Quick start

1. Install the extension.
2. Open a workspace containing a `gaffer.toml` file.
3. Click Run or Debug above any projection in `gaffer.toml`, or run `KurrentDB Projections: Run` from the command palette.

The extension spawns the [`@kurrent/gaffer` CLI](https://www.npmjs.com/package/@kurrent/gaffer) for LSP, MCP, and debug sessions, and will offer to install it on first use if it isn't already on PATH.

## Configuration

| Setting                        | Default      | What it does                                                                                                       |
| ------------------------------ | ------------ | ------------------------------------------------------------------------------------------------------------------ |
| `gaffer.command`               | `["gaffer"]` | Argv used to invoke gaffer. User scope only; workspace settings are ignored as defense against hostile workspaces. |
| `gaffer.debugPort`             | `-1` (auto)  | DAP server port (loopback only). `-1` lets the OS pick a free port and the editor reads it back on connect.        |
| `gaffer.injectProjectionTypes` | `true`       | Inject projection-runtime types via the tsserver plugin. Disable to keep non-projection JS clean.                  |

## Commands

| Command                                     | What it does                                                  |
| ------------------------------------------- | ------------------------------------------------------------- |
| `KurrentDB Projections: Run`                | Run the projection at the cursor against its declared fixture |
| `KurrentDB Projections: Debug`              | Run with the debugger attached                                |
| `KurrentDB Projections: Debug from Fixture` | Pick a fixture to debug                                       |
| `KurrentDB Projections: Stop`               | Stop the running session                                      |

## Telemetry

The extension collects anonymous usage telemetry by default and respects VS Code's `telemetry.telemetryLevel` setting. See [TELEMETRY.md](TELEMETRY.md) for the full list of what is collected and how to opt out.

## Documentation

Full documentation at <https://docs.kurrent.io/gaffer/>.

Bugs go to [GitHub Issues](https://github.com/kurrent-io/gaffer/issues). Questions and feature requests to [Discussions](https://github.com/kurrent-io/gaffer/discussions).

## License

[Kurrent License v1](LICENSE)
