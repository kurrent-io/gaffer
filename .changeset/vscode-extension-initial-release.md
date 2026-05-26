---
"gaffer-vscode": minor
---

Initial release of the KurrentDB Projections VS Code extension.

- Debug projections from `gaffer.toml` via CodeLens. Step over / into / out, breakpoints, full state inspection.
- Per-fixture debug entry points: each fixture in `gaffer.toml` gets its own CodeLens.
- Scaffold and Init palette commands. Right-click "Scaffold Projection Here" in the explorer.
- Type-aware autocomplete for projection builtins (`fromAll`, `when`, `emit`, `linkTo`, ...) via a tsserver plugin injected at extension load.
- MCP server auto-registration so AI assistants pick up gaffer's scaffolding, validation, debugging, and projection API tools.
- LSP-driven diagnostics for `gaffer.toml`.
- First-run install prompt for the `@kurrent/gaffer` CLI when it isn't on `PATH`.
- Update-available notification when a newer CLI version is published to npm.
- Anonymous usage telemetry respecting `telemetry.telemetryLevel`. See `TELEMETRY.md`.
