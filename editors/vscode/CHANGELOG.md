# gaffer-vscode

## 0.1.8

### Patch Changes

- bbfd56c: The extension now shows read-only deployment status in `gaffer.toml`, per environment and per projection.

  **Above each `[env.<name>]` block**, a roll-up of how that environment's projections compare to what's deployed, a **Sign in** action when it needs authentication, or a **status unavailable** note when the read can't complete. A production target is flagged **PRODUCTION**.

  **On each `[[projection]]` header**, a row of small status dots, one per environment in file order. A filled green, orange, or red dot means in sync, needs attention, or faulted/invalid. A hollow ring, crossed ring, or faint dot means the environment needs sign-in, couldn't be read, or is still loading. Hovering the header lists each environment's verdict and runtime state.

  Status is read on open and save, refreshes on its own once you sign in, and the projection dots also refresh on a timer while the file is visible, so they track live runtime state (a projection stopping, faulting, or catching up) without re-opening or saving. Polling is scoped to the visible config editors and stops when none are visible; it pauses while a file has unsaved edits and resumes on save. The language server keeps each poll cheap by reusing the cached drift verdict and reading only runtime state.

- bbfd56c: Each `[[projection]]` header in `gaffer.toml` now carries a **Manage...** lens that opens a per-projection action menu, grouped by environment: **History**, **Diff against deployed**, **Deploy**, and the operate verbs **Pause**, **Resume**, **Abort**, and **Delete**.

  - **Diff against deployed** opens VS Code's native diff editor comparing the projection's local source against what's deployed on the chosen environment. Both sides are read-only. A projection that isn't deployed shows a message instead of an empty diff.
  - **The operate verbs** run over the language server's warm connection and report the result. The menu shows Pause or Resume based on the projection's current runtime state, and deleting a projection that emits streams asks whether to remove those too. Each verb confirms before running: a non-production reversible verb runs straight away, a production or irreversible verb asks you to confirm, and deleting on production asks you to type the projection's name.

  **Each environment shows its own state, and collapses when it has nothing to offer.** One that needs authentication collapses to a single **Sign in**; one whose status couldn't be read collapses to a single **Unavailable**, rather than listing actions that can't run against an unreachable environment. The menu opens immediately and shows a **Loading status…** row for any environment still being read, filling in that environment's actions in place as the status resolves, so a still-loading environment is never a dead-end that needs closing and reopening.

- bbfd56c: Each `[env.X]` block in `gaffer.toml` gains a **Deploy** lens leading the block, ahead of its status roll-up, and the per-projection **Manage...** menu's **Deploy** action does the same for a single projection. Both open the deploy plan in an editor-area webview, to review and then deploy. The lens is scoped to the whole project against that env; the menu action is scoped to just that projection, resolving its own change and bundling a recreate when one's needed.

  - The plan lists every projection with its change (create, update, rebuild, recreate, unchanged, or invalid) and the warnings that matter: faulted, re-emits, a logic change, and a definition changed outside gaffer. It leads with the resolved target and a production pill, and surfaces any `[database_config]` divergence. An updated projection offers a **Diff** against what's deployed; an invalid one shows its compile error inline, wrapped in place rather than scrolling sideways.
  - **Deploy** applies the plan behind a native confirm. The tier follows the target: silent off production with no rebuild, a modal accept when production or a rebuild is involved, and typing the environment name for a production rebuild. Dismissing the confirm, deploying from an untrusted workspace, or a deploy that fails to start all leave the panel usable, with the Deploy button re-enabled.
  - A blocked plan can't deploy; off production, a checkbox deploys the valid projections and skips the rest (`--no-validate`). The apply streams each projection's row in place, then a result summary. It's a cold spawn on the same auth path as the preview, so what you review is what deploys.

  The untrusted-workspace notice names deploy and operate among the CLI-driven actions disabled until the workspace is trusted.

- bbfd56c: The per-projection **Manage...** menu gains a **History** action: a timeline of the projection's deploys on an environment, in an editor tab. Each version shows its operation, content hash, actor, and time, drawn as a graph that reads run state (enabled/disabled/deleted), reverts, and recreates. It's the same grammar as `gaffer history` in the terminal.

  **Two panes.** The timeline sits on the left; selecting a version opens a detail panel on the right with its metadata (when, run state, content hash, actor, tool, operation, and source) and its actions. Timeline rows are keyboard-navigable, and the panel drops below the timeline on a narrow editor. The panel takes the width its content needs from the timeline, up to a cap, keeping its action buttons on one line where there's room.

  **A version's actions** sit in that panel: diff it against the previous version or against your local source (both open VS Code's diff editor), or roll back to it. Rollback rewrites the live query to the chosen version behind the native confirm (silent off-production, a modal accept on production). State is kept and local files are left untouched, so they show as drift until updated. The timeline refreshes as soon as a rollback lands, rather than waiting on the success toast being dismissed, and history and rollback get a spawn timeout long enough for a slow or remote cluster, since connecting, resolving a version, and reading or writing didn't fit the 10s default.

  **Attribution follows the CLI.** A version is flagged as changed outside gaffer from the `outOfBand` field rather than from the kind, and only once gaffer has been managing the projection, so writes on a server that doesn't preserve gaffer's metadata no longer all show as external edits. A metadata-less update shows what it changed, e.g. `query changed`, matching the terminal; an out-of-band edit reads `query changed outside gaffer` rather than the less specific `changed outside gaffer`.

- bd7bc05: Editor surfaces are now hidden when the installed `gaffer` CLI can't run them, rather than offered and failing on click. The extension updates from the marketplace while the CLI is installed separately, so a new extension routinely drives an older `gaffer`.

  - The env-block **Deploy** lens is hidden unless the CLI can run the deploy spawns. The per-projection **Manage...** menu drops any row its CLI can't serve: **Deploy** and **History** are checked against the CLI's own command listing, **Diff against deployed** and the operate verbs against the methods the language server advertises. An environment left with no available rows collapses to a single **Unsupported** notice, matching the existing **Unavailable** and **Sign in** collapses. The **Manage...** lens itself is hidden when nothing at all is available.
  - **Rollback** from the history viewer reports that it needs a newer CLI instead of failing the spawn, and does so before the confirmation prompt rather than after. Reading the timeline and rolling back are separate capabilities, so a CLI that can only read still gets a working read-only history.
  - Deploy status is no longer polled against a language server that doesn't implement it. The poll previously wrote a `MethodNotFound` error into the **Gaffer (LSP)** output channel every five seconds for the whole session against an older CLI.
  - An action that does reach a server without it now says the CLI needs updating, instead of surfacing the server's internal "method not implemented" wording.

- 40d8b96: The extension isolates its encrypted-file token store at `keyring-vscode` (via `GAFFER_KEYRING_NAME`) on a host with no OS keyring, keeping it separate from the CLI's default store. The random passphrase the extension injects to unlock its own store therefore never locks the store a manual terminal `gaffer` uses.
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
