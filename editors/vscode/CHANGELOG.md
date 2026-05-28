# gaffer-vscode

## 0.1.3

### Patch Changes

- 1e3f438: The `gaffer not installed` prompt no longer persists on Windows after `npm install -g @kurrent/gaffer`. CLI spawn sites now route through `cross-spawn`, which honours `PATHEXT` and resolves the `gaffer.cmd` shim that npm drops into `%APPDATA%\npm`.

## 0.1.2

### Patch Changes

- 824d6b9: Fix broken banner image on the marketplace listing.

## 0.1.1

### Patch Changes

- e02eaf4: Fix packaging.

## 0.1.0

### Minor Changes

- f897305: Initial release of the KurrentDB Projections VS Code extension.
  - Debug projections from `gaffer.toml` via CodeLens. Step over / into / out, breakpoints, full state inspection.
  - Per-fixture debug entry points: each fixture in `gaffer.toml` gets its own CodeLens.
  - Scaffold and Init palette commands. Right-click "Scaffold Projection Here" in the explorer.
  - Type-aware autocomplete for projection builtins (`fromAll`, `when`, `emit`, `linkTo`, ...) via a tsserver plugin injected at extension load.
  - MCP server auto-registration so AI assistants pick up gaffer's scaffolding, validation, debugging, and projection API tools.
  - LSP-driven diagnostics for `gaffer.toml`.
  - First-run install prompt for the `@kurrent/gaffer` CLI when it isn't on `PATH`.
  - Update-available notification when a newer CLI version is published to npm.
  - Anonymous usage telemetry respecting `telemetry.telemetryLevel`. See `TELEMETRY.md`.
