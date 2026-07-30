<img src="https://raw.githubusercontent.com/kurrent-io/gaffer/main/editors/vscode/media/banner.png" alt="KurrentDB Gaffer" width="100%">

Projection debugger and CodeLens companion for [KurrentDB](https://www.kurrent.io). Run and debug projections from `gaffer.toml`, step through handlers with full state inspection, deploy and operate them against your environments, and get type-aware autocomplete for projection builtins.

Powered by the [gaffer](https://www.npmjs.com/package/@kurrent/gaffer) toolkit.

## Features

**Debug projections from `gaffer.toml`.** CodeLens above each projection and fixture block. Click Debug to launch a session locally. Set breakpoints to pause for inspection, or let it run through.

**Step through handlers.** Set breakpoints in your projection JS. Step over, into, and out of handlers. Watch state evolve event by event in the dedicated panel.

**Live state inspection.** The Gaffer panel shows the current step, partitioned state, shared state (for biState projections), emitted events, and console output as the projection runs.

**Deployment status in `gaffer.toml`.** Each `[env.<name>]` block shows a summary of how your projections compare to what's deployed on that environment, and each `[[projection]]` header carries per-environment status dots (in sync, drifted, faulted or invalid, needs sign-in). Read-only, refreshed on open and save, and kept current while the file is visible.

**Deploy plans, confirmed in tiers.** A Deploy lens opens the plan in an editor tab: create, update, rebuild, or recreate per projection, with deployed-vs-local diffs and compile errors inline. Applying confirms to match the target, from silent off production up to typing the environment name for a production rebuild.

**History and rollback.** A projection's deploy timeline opens in an editor tab, each version with its operation, content hash, actor, and time. Diff any version against the previous one or your local source, or roll back to it behind a native confirm (silent off production, a modal accept on production).

**Operate from the editor.** A Manage lens on each projection groups its actions by environment: deploy, history, diff against deployed, and the operate verbs (pause, resume, abort, delete). OAuth environments offer Sign in, which runs `gaffer auth` in a terminal; you sign in through the browser once, and the stored token refreshes automatically for later runs.

**Type-aware autocomplete for projection builtins.** A TypeScript server plugin injects `fromStream`, `when`, `emit`, `linkTo`, `partitionBy`, and the rest of the projection API into JavaScript files that share a workspace root with a registered projection. No imports needed.

**MCP server auto-registration.** The gaffer MCP server (scaffolding, validation, debugging tools, deploy and operate tools, projection API resources) is auto-registered with VS Code's MCP framework. Available to GitHub Copilot, Claude, and any other MCP-aware tooling.

## Quick start

1. Install the extension.
2. Open a folder and add projections via `Gaffer: Scaffold` (palette) or right-click a folder and pick `Scaffold Projection Here`. A fresh folder gets a `gaffer.toml` created for it automatically as part of the first scaffold; run `Gaffer: Init` from the palette if you'd rather create the toml without scaffolding.
3. Click Debug above any projection in `gaffer.toml`, or run `Gaffer: Debug` from the command palette.

The extension spawns the [`@kurrent/gaffer` CLI](https://www.npmjs.com/package/@kurrent/gaffer) for LSP, MCP, and debug sessions. It will offer to install the CLI on first use if it isn't already on `PATH`. The deploy and operate surfaces follow the installed CLI's capabilities: an extension newer than its `gaffer` hides actions the CLI can't run, and updating the CLI restores them.

## Configuration

| Setting                         | Default      | What it does                                                                                                             |
| ------------------------------- | ------------ | ------------------------------------------------------------------------------------------------------------------------ |
| `gaffer.command`                | `["gaffer"]` | Argv used to invoke gaffer. User scope only; workspace settings are ignored as defense against hostile workspaces.       |
| `gaffer.debugPort`              | `-1` (auto)  | DAP server port (loopback only). `-1` lets the OS pick a free port and the editor reads it back on connect.              |
| `gaffer.injectProjectionTypes`  | `true`       | Inject projection-runtime types via the tsserver plugin. Disable to keep non-projection JS clean.                        |
| `gaffer.cliUpdateNotifications` | `true`       | Surface a status bar prompt when a newer gaffer CLI is available on npm. The prompt's "Never ask" option flips this off. |

## Commands

| Command                          | What it does                                                                                              |
| -------------------------------- | --------------------------------------------------------------------------------------------------------- |
| `Gaffer: Init`                   | Create a `gaffer.toml` in the current folder                                                              |
| `Gaffer: Scaffold`               | Add a new projection to the project via a multi-step picker. Right-clicking a folder runs it scoped there |
| `Gaffer: Debug`                  | Pick a projection from the workspace and launch a debug session                                           |
| `Gaffer: Debug from...`          | Codelens-triggered picker to debug against a fixture or a configured environment                          |
| `Gaffer: Stop`                   | Stop the running session                                                                                  |
| `Gaffer: Deploy Plan`            | Open an environment's deploy plan in an editor tab                                                        |
| `Gaffer: Projection Actions`     | Per-projection menu: deploy, history, diff against deployed, and the operate verbs, by environment        |
| `Gaffer: Projection History`     | Open a projection's deploy timeline on an environment                                                     |
| `Gaffer: Sign In to Environment` | Sign in to an OAuth environment (runs `gaffer auth` in a terminal)                                        |

The deploy and operate commands are reached from their CodeLenses in `gaffer.toml` rather than the palette; each needs a projection or environment to act on.

## Telemetry

The extension collects anonymous usage telemetry by default and respects VS Code's `telemetry.telemetryLevel` setting. See [TELEMETRY.md](TELEMETRY.md) for the full list of what is collected and how to opt out.

## Documentation

Full documentation at <https://gaffer.kurrent.io/>.

Bugs go to [GitHub Issues](https://github.com/kurrent-io/gaffer/issues). Questions and feature requests to [Discussions](https://github.com/kurrent-io/gaffer/discussions).

## License

[Kurrent License v1](LICENSE)
