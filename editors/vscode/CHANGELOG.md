# gaffer-vscode

## 0.1.5

### Patch Changes

- b2b05f1: Scaffold from the command palette now skips the partitioning step for a single-stream source, where per-stream partitioning isn't valid, matching the CLI.

## 0.1.4

### Patch Changes

- 652947b: Diagnostics now link to a reference page at [gaffer.kurrent.io/reference/diagnostics](https://gaffer.kurrent.io/reference/diagnostics/), generated from the diagnostic catalog with one entry per `quirk.*` / `usage.*` code. The `gaffer dev` and test summaries print a `See <url>` line after the quirk list, and on interactive terminals each diagnostic code is itself a hyperlink to its entry. The VS Code step-warning panel makes each quirk a clickable link too.
- 9f9722a: The VS Code Step panel now shows the `quirk.*` / `usage.*` diagnostic codes introduced by the diagnostics taxonomy rename in this release.
- afb3edc: The extension's marketplace title is now "KurrentDB Gaffer (Projections tooling)" and all in-editor surfaces use the short brand "Gaffer". Command-palette entries read `Gaffer: Debug`, `Gaffer: Scaffold`, and so on; the output channels, panel, and notifications say "Gaffer" instead of "KurrentDB Projections".
- d59611f: The debug Step panel now shows the runtime quirks that fired while processing an event. Each `gaffer/stepWarning` from the CLI appears as a warning node under the step, inline with the handler's logs and emitted events in the order they happened. Stepping through a projection surfaces a quirk as you hit it. Runtime quirks stay off the Problems panel by design: they are value-dependent and have no source range, so they belong on the execution surface rather than the static-analysis one.

  The Status view also tallies the distinct runtime quirks seen so far, alongside the processed and error counts.

- fc48c10: Clicking **Debug** on Windows no longer fails with a misleading "Timeout waiting for debug message". The IPC debug spawn now routes through `cross-spawn`, which resolves the npm-installed `gaffer.cmd` shim, and a spawn that never starts surfaces immediately as an exit instead of waiting out the full timeout.

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
