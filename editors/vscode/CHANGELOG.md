# gaffer-vscode

## 0.1.8

### Patch Changes

- 5269bf2: The per-projection **Manage...** menu lists **History** above **Diff against deployed**.
- 9711fb2: Each `[env.X]` block in `gaffer.toml` gains a **Deploy** lens leading the block, ahead of its status roll-up. It opens the deploy plan for the whole project against that env in an editor-area webview, to review and then deploy.
  - The plan lists every projection with its change (create, update, rebuild, recreate, unchanged, or invalid) and the warnings that matter: faulted, re-emits, a logic change, and a definition changed outside gaffer. It leads with the resolved target and a production pill, and surfaces any `[database_config]` divergence. An updated projection offers a **Diff** against what's deployed; an invalid one shows its compile error inline.
  - **Deploy** applies the plan behind a native confirm. The tier follows the target: silent off production with no rebuild, a modal accept when production or a rebuild is involved, and typing the environment name for a production rebuild.
  - A blocked plan can't deploy; off production, a checkbox deploys the valid projections and skips the rest (`--no-validate`). The apply streams each projection's row in place, then a result summary. It's a cold spawn on the same auth path as the preview, so what you review is what deploys.

- 9cdf7a5: The per-projection **Manage...** menu gains a **Deploy** action alongside diff and the operate verbs. It opens the deploy plan scoped to just that projection against the chosen environment, in the same editor-tab webview as the whole-project Deploy lens, and applies it behind the same native confirm. The single-projection plan resolves its own change and bundles a recreate when one's needed.
- 8a8db34: The extension now shows read-only deployment status above each `[env.<name>]` block in `gaffer.toml`. Each environment gets a roll-up of how its projections compare to what's deployed, a **Sign in** action when it needs authentication, or a **status unavailable** note when the read can't complete. A production target is flagged **PRODUCTION**. Status is read on open and save, and refreshes on its own once you sign in.
- 17f9ea0: Each `[[projection]]` header in `gaffer.toml` now carries a **Manage...** lens that opens a per-projection action menu, grouped by environment. Its first action, **Diff against deployed**, opens VS Code's native diff editor comparing the projection's local source against what's deployed on the chosen environment. Both sides are read-only. A projection that isn't deployed shows a message instead of an empty diff, and an environment that needs authentication offers a one-click sign-in.
- 5d5b707: The history viewer now shows what a metadata-less update changed, e.g. `query changed`, matching the terminal `gaffer history`. An out-of-band edit reads `query changed outside gaffer` rather than the less specific `changed outside gaffer`.
- 62a20e7: The history viewer follows the CLI's revised attribution. It flags a version as changed outside gaffer from the `outOfBand` field rather than from the kind, and only after gaffer has been managing the projection. So writes on a server that doesn't preserve gaffer's metadata no longer all show as external edits.
- afe837c: The deploy-history viewer now uses a two-pane layout. The timeline stays on the left; selecting a version opens a detail panel on the right showing its metadata (when, run state, content hash, actor, tool, operation, and source) and its actions. The diff-previous, diff-local, and roll-back actions moved off each row into that panel. Timeline rows are keyboard-navigable, and the panel drops below the timeline on a narrow editor.
- 5269bf2: The history viewer's rollback is more reliable:
  - It no longer times out against a slow or remote cluster. History and rollback connect, resolve a version, and read/write, which the 10s default spawn timeout was too short for; they now get a longer one.
  - The timeline refreshes as soon as a rollback lands. The refresh was previously sequenced after the success notification, which only resolves when the toast is dismissed, so the timeline appeared stale until then.

- c967952: The per-projection **Manage...** menu gains a **History** action: a timeline of the projection's deploys on an environment, in an editor tab. Each version shows its operation, content hash, actor, and time, drawn as a graph that reads run state (enabled/disabled/deleted), reverts, and recreates. It's the same grammar as `gaffer history` in the terminal.
  - A content version's actions sit alongside it: diff it against the previous version or against your local source (both open VS Code's diff editor), or roll back to it.
  - Rollback rewrites the live query to the chosen version behind the native confirm (silent off-production, a modal accept on production). State is kept and local files are left untouched, so they show as drift until updated.

- 40d8b96: The extension isolates its encrypted-file token store at `keyring-vscode` (via `GAFFER_KEYRING_NAME`) on a host with no OS keyring, keeping it separate from the CLI's default store. The random passphrase the extension injects to unlock its own store therefore never locks the store a manual terminal `gaffer` uses.
- 1159470: The per-projection **Manage...** menu now updates live while an environment's status is still loading. It opens immediately, shows a **Loading status…** row for any environment still being read, and fills in that environment's actions in place as the status resolves - so a still-loading environment is no longer a dead-end that needed closing and reopening.
- 799a07c: The per-projection **Manage...** menu now offers the operate verbs alongside the diff: **Pause**, **Resume**, **Abort**, and **Delete**. Deleting a projection that emits streams asks whether to remove those too. The menu shows Pause or Resume based on the projection's current runtime state. Each environment shows its status, and one that needs authentication offers Sign in instead of the actions. Each verb confirms before running: a non-production reversible verb runs straight away, a production or irreversible verb asks you to confirm, and deleting on production asks you to type the projection's name. It then runs over the language server's warm connection and reports the result.
- 8fdd029: Each `[[projection]]` header in `gaffer.toml` now shows a row of small status dots, one per environment in file order. A filled green, orange, or red dot means in sync, needs attention, or faulted/invalid. A hollow ring, crossed ring, or faint dot means the environment needs sign-in, couldn't be read, or is still loading. Hovering the header lists each environment's verdict and runtime state. Read-only, read on open and save, like the environment summary.
- b143049: The per-projection status dots in `gaffer.toml` now refresh on a timer while the file is visible, so they track live runtime state (a projection stopping, faulting, or catching up) without re-opening or saving. Polling is scoped to the visible config editors and stops when none are visible. It also pauses while a file has unsaved edits, resuming on save. The language server keeps each poll cheap by reusing the cached drift verdict and reading only runtime state.
- 9cdf7a5: In the per-projection **Manage...** menu, an environment whose status couldn't be read now collapses to a single **Unavailable** notice instead of listing actions that can't run against an unreachable environment, matching how an environment that needs authentication collapses to a single **Sign in**.
- 9e79e06: The extension's webviews (status, deploy-plan, history) now report client-side render failures to telemetry. A webview has no network egress of its own, so uncaught errors, unhandled rejections, and render errors caught by its error boundary are forwarded to the extension host. The host emits them as `exception` events under a new `webview` phase. Messages and stack frames are scrubbed the same as host-side exceptions; nothing from your projection code is reported.

## 0.1.7

### Patch Changes

- aa31ec5: Exception telemetry now redacts connection-string credentials and hosts from error messages before they leave your machine, extending the filesystem-path scrubbing already applied to these messages. A URL carrying credentials (e.g. `esdb://user:pass@cluster:2113`) is reduced to `esdb://<redacted>`, keeping only the scheme and path.
- 7a3d067: Exception telemetry now strips filesystem paths from error messages before they leave your machine. OS-level errors (e.g. a permission-denied `stat`) embed absolute paths that could include your username; these are now replaced with `<path>`, matching the telemetry notice's existing "no paths or error messages" promise. Stack frames were already scrubbed to basenames.

## 0.1.6

### Patch Changes

- 22d9480: Debug CodeLenses are now environment-aware. The projection-level **Debug** lens runs live against the default environment, or the sole configured one, and is hidden when there's no unambiguous target so it no longer faults. **Debug from fixture...** becomes **Debug from...**: a single picker offering the projection's fixtures and every configured environment, so a non-default environment is reachable without editing `gaffer.toml`.
- a01807a: The command palette **Gaffer: Debug** now offers configured environments, matching the CodeLens picker. After you choose a projection it lists every fixture and every `[env.<name>]`, with the default tagged, so a non-default environment is reachable from the palette. Previously it only knew the default connection, leaving multi-environment projects unable to pick another env there.
- 33e3b4b: `gaffer scaffold` now lets you choose the new projection's engine version (`1` or `2`, default `2`). It's a `--engine-version` flag and an interactive prompt on the CLI, an `engine_version` argument on the MCP `scaffold` tool, and a step in the VS Code scaffold wizard.

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
