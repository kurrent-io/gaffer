# @kurrent/gaffer

## 0.4.1

### Patch Changes

- 6648f57: `gaffer.toml` is now written atomically (temp file + rename) instead of rewritten in place. A reader that re-reads the manifest on change (the LSP file watcher, the MCP server) can no longer catch a half-written file, and a crash mid-write can no longer truncate it.
- abca69e: `gaffer dev`, the MCP tools (`get_state`, `run`, `debug`), and the DAP `gaffer/partitionState` request now surface state-getter errors instead of silently returning partial or empty state. A throwing V1 `transformBy`/`filterBy` during state collection previously looked identical to an absent value. `get_state` now returns a tool error, `run`/`debug` results carry a `stateError` field when state collection fails, the DAP partition-state request returns an error response, and `gaffer dev` prints a `warning: reading projection state: ...` line while still showing the summary.
- 9948be5: `gaffer dev` now rejects contradictory source flags instead of silently dropping one. An offline source (`--fixture` / `--events`) can't be combined with a live target (`--env` / `--connection`). Previously `gaffer dev p --fixture happy --env cloud` ran the fixture and ignored `--env`. `--env` and `--connection` may still be combined, where `--connection` overrides `--env`.
- 592db24: The startup `.env` auto-load no longer walks above `$HOME` to find the project root. A stray `gaffer.toml` in a shared ancestor (a world-writable `/tmp`, or `/home` on a multi-user host) could otherwise make its `.env` (including `KURRENTDB_USERNAME` / `KURRENTDB_PASSWORD`) ambient for every `gaffer` invocation below it. The walk now stops at `$HOME`, matching the telemetry opt-out walk; the telemetry project-id walk is bounded the same way.
- 683e9e5: `gaffer mcp` no longer crashes when a session is torn down while a tool call is in flight. Concurrent tool calls that race a session teardown (for example `stop` while a `run` is parked at a breakpoint) previously could panic the whole MCP server or use-after-free the native session. Teardown is now serialised, a parked handler whose session was stopped returns a clean "session was stopped" error, and any residual handler panic is reported as a tool error instead of taking the process down.
- afafbff: `get_timeline` no longer fails with a raw `SQL logic error: no such table: steps` after a live `run`. The in-memory history store now pins itself to a single connection, so concurrent inserts from a live subscription and timeline queries always see the same database. When a session recorded no steps, `get_timeline` now reports "No timeline recorded for this session." instead of an empty range.
- 2374f05: A live `run` that times out before catching up no longer reports "timed out waiting for breakpoint" when no breakpoint was set. The `run` tool now names the actual condition (catching up to the head of the stream, hitting a breakpoint, or both), reports how many events were processed, and notes that the session is still running so it can be inspected with `get_state` / `get_timeline` or ended with `stop`.

## 0.4.0

### Minor Changes

- 33e3b4b: **Breaking:** `gaffer.toml` now models connections as named environments, and `engine_version` is set per projection. Top-level `connection` and top-level `engine_version` are no longer supported; loading a file with either fails with a migration hint.

  To migrate, move the top-level `connection` into an `[env.<name>]` block (mark one `default = true`), and set `engine_version` on each `[[projection]]`:

  ```toml
  # before
  connection = "kurrentdb://localhost:2113?tls=false"
  engine_version = 2

  [[projection]]
  name = "order-count"
  entry = "projections/order-count.js"

  # after
  [env.local]
  connection = "kurrentdb://localhost:2113?tls=false"
  default = true

  [[projection]]
  name = "order-count"
  entry = "projections/order-count.js"
  engine_version = 2
  ```

  Each `[env.<name>]` carries its own `connection`, and exactly one may set `default = true` (used when `--env` is omitted). Environment names must match `^[A-Za-z0-9_-]+$`.
  - `gaffer dev` gained `--env <name>` to select an environment; `--connection` is an ad-hoc override that beats both `--env` and the configured environment. The MCP `list_events` and live `run` tools take the same `env` argument.
  - A per-environment `.env.<env>` file overlays the base `.env`, so each environment can carry its own credentials. The precedence, highest first, is the shell environment, then `.env.<env>`, then the base `.env`. Both `${VAR}` references in a connection and the `KURRENTDB_USERNAME` / `KURRENTDB_PASSWORD` credentials resolve from those sources.
  - `gaffer init` no longer takes `--engine-version` or `--yes`; it writes a commented starter template.

### Patch Changes

- 327fc30: `gaffer dev` resolves event sources more helpfully when `gaffer.toml` defines environments. The interactive source picker now offers each configured environment as a live option, not just the `default` one, so a single non-default environment is selected automatically and multiple are pickable. When no source resolves non-interactively, the error names the available environments and suggests `--env <name>` or `default = true`, rather than pointing you to configure an `[env.<name>]` you may already have.
- 3324def: `.env` is now loaded into the process environment at startup, so a project `.env` applies on every code path, not only after a database connection is made.
  - Env-var opt-outs (`GAFFER_TELEMETRY_OPTOUT`, `KURRENTDB_TELEMETRY_OPTOUT`, `DO_NOT_TRACK`, `GAFFER_NO_UPDATE_CHECK`) set in `.env` are now honoured. Previously they were read only from the shell environment.
  - The `connection` string in `gaffer.toml` supports `${VAR}` expansion (braced form only), so credentials can stay out of the committed file. An undefined variable is an error; a bare `$` is left untouched.
  - The shell environment wins over `.env`: a value already set in the shell, or injected by CI, is never overwritten.

- 33e3b4b: `gaffer scaffold` now lets you choose the new projection's engine version (`1` or `2`, default `2`). It's a `--engine-version` flag and an interactive prompt on the CLI, an `engine_version` argument on the MCP `scaffold` tool, and a step in the VS Code scaffold wizard.

## 0.3.1

### Patch Changes

- 430c78d: `gaffer init`, `gaffer scaffold`, and `gaffer dev` now prompt interactively when run on a terminal, asking only for values not already supplied as flags or positionals.
  - `gaffer init` prompts for the engine version and gains an `--engine-version <1|2>` flag (default `2`).
  - `gaffer scaffold` prompts for the path (when omitted) plus source, partitioning, and emit, offering only partitioning options valid for the chosen source.
  - `gaffer dev` prompts for the projection (when omitted) and the event source when none is given via `--events` / `--fixture` / `--connection`.
  - `gaffer scaffold` and `gaffer dev` gain `--yes` / `-y` to skip prompts (the projection path / name must then be supplied as arguments). On `gaffer init`, `-y` now skips the prompt and uses the default engine version, rather than being a no-op.
  - `gaffer scaffold` now rejects per-stream partitioning on a single-stream source up front, instead of generating a projection that only fails when run.

  Piped and non-interactive (CI) invocations are unchanged: they never prompt, so existing scripts keep working.

## 0.3.0

### Minor Changes

- 9f9722a: Diagnostic codes now use one `quirk.*` / `usage.*` taxonomy. Every diagnostic has a three-segment code `<class>.<subject>.<detail>`, where `quirk.*` reproduces a KurrentDB engine bug and `usage.*` flags something about your own projection.

  This is a breaking rename of the diagnostic codes surfaced on `FeedResult.diagnostics`, `ProjectionInfo.diagnostics`, the testing library's `step.diagnostics`, and the CLI/MCP output:
  - `compat.linkStreamTo.outOfBoundsParameters` → `quirk.linkStreamTo.outOfBoundsParameters`
  - `compat.log.multiParam` → `quirk.log.multiParam`
  - `compat.event.bodyCast` → `quirk.event.bodyCast`
  - `compat.serialize.nonFinite` → `quirk.serialize.nonFinite`
  - `compat.transforms.notInvoked` → `usage.transforms.notInvoked`
  - `compat.outputState.unconditional` → `quirk.outputState.noEffectOnV2`
  - `deprecated.linkStreamTo` → `usage.linkStreamTo.deprecated` (now Information, was Warning)
  - `options.duplicate` → `usage.options.duplicate`
  - `handler.async` → `usage.handler.async`
  - `handler.promise` → `usage.handler.promise`

  Other changes in this release:
  - **Severity is Error / Warning / Information only.** The unused `Hint` level (LSP 4) is dropped from `DiagnosticSeverity`. Severity follows a per-firing rubric: Error when there is no correct form (it throws or is unsupported), Warning when it runs but produces a wrong result, Information when it works but is noteworthy.
  - **`reorderEvents` is engine-version aware.** Under `engine_version 1`, an invalid reordering config (not `fromStreams()` with 2+ streams, or `processingLag` below 50ms) is rejected at session create, matching KurrentDB's `ReaderStrategy`. Under `engine_version 2` it has no effect and surfaces as a `usage.reorderEvents.noEffectOnV2` warning rather than the old unconditional error. This replaces the `options.fromStreamsOnly` diagnostic.
  - **Throwing quirks now also raise a diagnostic.** A quirk that throws (e.g. `quirk.event.bodyCast`, `quirk.serialize.nonFinite`) exposes a `diagnostics` array on the thrown error, surfaced on the Go error types and the JS `ProjectionError` and propagated through the testing library. The array carries the quirk that threw plus any that fired earlier in the same event, so it is the complete set where `compatCode` is just the throwing quirk's code. Errors are also enriched with `compatDescription` and `compatFixedIn`.
  - **Quirk-catalogue exports are removed.** The catalogue is no longer exported over FFI: `knownQuirks()` (and the `KnownQuirk` type) is gone from the JS runtime binding, and `KnownQuirks()` / `KnownQuirk` / `DiagnosticSeverityHint` are gone from the Go binding. Assert on `step.diagnostics` (the data plane) instead.
  - **Diagnostics trued up against KurrentDB 26.2.0 (PR #5610).** `quirk.event.bodyCast` and `quirk.serialize.nonFinite` are marked fixed in 26.2.0 and no longer fire when targeting that version. The `biState.stringSlot` / `biState.sharedStringSlot` quirks are **removed**: JSON-encoding a string state-array slot is correct KurrentDB behaviour, not a bug. The real bug is the new `quirk.serialize.rawString`: a bare string state that isn't valid JSON is persisted un-encoded and faults on reload (also fixed in 26.2.0).
  - **New `engine_version 2` diagnostics.** `quirk.biState.sharedStateResetOnV2` flags bi-state / `$initShared` projections on V2, where shared state is silently re-initialized on restart. `trackEmittedStreams` on V2 is rejected at session create, matching KurrentDB. `outputState()` on V2 is now `quirk.outputState.noEffectOnV2` (Warning, was `usage.outputState.unconditional` Information). V2 emits no result streams, with parity planned for a future release.

- e9dfaff: The quirks-selecting option and the quirk registry are renamed to retire the misleading "DB version" / "bug" framing.
  - **`dbVersion` is now `quirksVersion`** across the runtime, the JS bindings (`SessionOptions`), and the testing library (`ProjectionOptions`). The value is unchanged: a `MAJOR.MINOR.PATCH` string, where unset still reproduces every known quirk and a set version turns off quirks fixed upstream as of it. Only the key moves. `dbVersion` read as passive info when it actively selects which quirks to emulate, and it collided with `engineVersion`.
  - **`knownBugs()` is now `knownQuirks()`**, and **`KnownBug` is now `KnownQuirk`**, in the JS and Go bindings. Most registry entries are deliberate KurrentDB quirks gaffer reproduces, not bugs to report upstream.
  - **CLI**: the `gaffer.toml` key `db_version` is now `quirks_version`, the env var `GAFFER_DB_VERSION` is now `GAFFER_QUIRKS_VERSION`, and the MCP resource `gaffer://docs/db-version-bugs` is now `gaffer://docs/quirks`. The connected-server-version telemetry (the `db_version` event property) is unaffected, since it genuinely reports the connected DB version.

  No deprecation period: pre-1.0, hard break. An old `dbVersion` or `db_version` key is silently ignored rather than rejected, so update existing call sites and `gaffer.toml` files.

### Patch Changes

- cf26d46: Projection handlers that use `async` or return a `Promise` now produce a compile-time warning. The projection engine is synchronous (no event loop), so it serializes the returned `Promise` as the state instead of awaiting it, leaving the state as `{}`. This matches KurrentDB but is surprising when authoring tests in an async-capable JS runtime, so gaffer flags it. The `Promise` check is a literal-syntax heuristic (`new Promise(...)`, `Promise.resolve(...)`, and similar).
- ad942bb: `gaffer scaffold`, `dev`, and `info` now report a missing or extra positional argument by naming the argument and showing a runnable example, instead of cobra's generic `Accepts 1 arg(s), received 0.`:

  ```
  missing required argument <path>
  example: gaffer scaffold ./projections/order.js
  ```

  Their `--help` gains an example too, and `dev`/`info` now show the required argument as `<projection>` rather than `[projection]`.

- 627dd02: `gaffer dev` now surfaces runtime quirks at the point they fire while processing an event, such as a `biState` string slot being JSON-quoted on persistence or a multi-argument `log()` call. In text output each quirk prints inline, interleaved with the handler's `log()` lines and emits in the order they happened, so stepping through a projection shows the warning as you hit the line. The JSON result line still carries a `diagnostics` array, and the run summary tallies every distinct quirk, compile-time and runtime alike. A `gaffer/stepWarning` DAP event also fires live per quirk, so editor integrations can attach the warning to the step.
- d59611f: The `gaffer dev` DAP `gaffer/stats` event now carries a `quirks` count: the number of distinct runtime-quirk codes seen so far in the session. This lets an editor tally fired quirks in its status view without tracking the per-step warnings itself.
- 652947b: Diagnostics now link to a reference page at [gaffer.kurrent.io/reference/diagnostics](https://gaffer.kurrent.io/reference/diagnostics/), generated from the diagnostic catalog with one entry per `quirk.*` / `usage.*` code. The `gaffer dev` and test summaries print a `See <url>` line after the quirk list, and on interactive terminals each diagnostic code is itself a hyperlink to its entry. The VS Code step-warning panel makes each quirk a clickable link too.
- 627dd02: `gaffer dev` text output now prints a handler's `log()` lines and emitted events under their own event header, in the order they happened, instead of before the header. The header is deferred until the result is known (so skipped events can be dropped silently), but logs and emits produced during processing now flush that header first.
- 9411111: The runtime and testing library now report three previously cryptic errors with friendlier messages:
  - `foreachStream()` on a `fromStream()` or `fromStreams()` projection now fails with "foreachStream() is only supported with fromAll() and fromCategory()", instead of a raw "Property 'foreachStream' of object is not a function" engine error.
  - A second `options()` call now produces a compile-time warning, since only the last call takes effect and the earlier ones are discarded silently.
  - The testing library now names which event shape was attempted and which field is wrong when a fed event matches none, instead of valibot's cryptic `Expected Object but received Object`.

- 2102508: The MCP server gains an `init` tool, so an assistant can create a gaffer project without leaving the protocol. Previously a project-less server could read the docs but had no in-protocol way to bootstrap one.
  - `init` creates a `gaffer.toml` in the server's project directory (the `--project` / `GAFFER_PROJECT` override, otherwise the working directory). The projection tools then resolve it on the next call, with no restart.
  - It refuses to run when a project is already in scope, naming where one was found, so it never shadows an existing project with a nested copy.
  - `gaffer init` and the tool now share one implementation, so they can't drift on what a fresh project looks like.

- 31a9b89: `gaffer mcp` can now be pointed at a project explicitly, instead of only searching upward from the working directory. This matters when the server is registered globally and launched from an arbitrary directory.
  - A `--project <dir>` flag and a `GAFFER_PROJECT` environment variable, each accepting a project root or any directory inside it (gaffer walks up to find the `gaffer.toml`).
  - Precedence: `--project` over `GAFFER_PROJECT` over the working-directory search.
  - When the override points somewhere without a `gaffer.toml`, the server still starts; the project tools' error names the path you gave so the misconfiguration is obvious.

- 6a441f8: `gaffer mcp` re-reads `gaffer.toml` on each project-dependent tool call instead of caching it for the session. Editing the manifest mid-session (adding a projection, fixing a connection string) is picked up by the next call with no restart; an invalid manifest surfaces a load error rather than silently serving the last good config.
- b0242e3: The MCP server now surfaces the runtime quirks that fired while processing an event, so an assistant can spot a fired quirk and act on it. `get_step` gains a top-level `diagnostics` array of the full quirk objects, and `get_timeline` / `get_history` carry the distinct quirk codes (`quirks`) per step. Each code cross-references the existing `gaffer://docs/quirks` resource, which explains the quirk and names a `quirksVersion` that opts out where one exists.
- 82b73f3: `gaffer mcp` now starts even when there is no `gaffer.toml` in the working directory, instead of failing during the MCP handshake. This makes the server safe to install as a global plugin, where the launch directory is arbitrary.
  - The documentation resources (`projection-api`, `gotchas`, `examples`, `quirks`) and `get_version` work without a project.
  - Project-dependent tools (`run`, `validate`, `list_projections`, `scaffold`, `get_projection_info`, `list_events`, debug) return a tool error pointing at `gaffer init` rather than taking the server down.
  - The project is resolved lazily, so creating a `gaffer.toml` mid-session is picked up on the next tool call without restarting the server.

  A `gaffer.toml` that exists but fails to parse or validate still surfaces as a startup error.

- c5d77a1: `gaffer mcp` usage telemetry now records a `started_in_project` flag, distinguishing sessions launched inside a project from project-less ones (for example a globally-registered server started outside any project).

  Manifest features are now also recorded for sessions that resolve their project lazily mid-run, for example after the `init` tool creates one. Previously those sessions left `manifest_features_used` unset.

- 1458673: `gaffer.toml` handling of `engine_version` has two fixes:
  - `gaffer scaffold` (and any command that re-saves the manifest) no longer writes `engine_version = 0` for projections with no engine version set. Previously the line was stamped on save, including onto existing projections.
  - An explicit `engine_version = 0` is now rejected with "must be 1 or 2, got 0" instead of being silently treated as unset.

- 47cfe96: Setting `reorderEvents` or `processingLag` on a projection whose source is not `fromStreams()` now produces a compile-time error diagnostic. These options only apply to `fromStreams([])`: KurrentDB rejects `reorderEvents` on other sources at subscription time, and `processingLag` has no effect without it. Gaffer previously accepted both on any source and silently ignored them.
- b217c5e: The runtime now builds with `InvariantGlobalization` enabled, so error messages stay English regardless of the host machine's locale. Previously a non-English-preference machine produced partially-translated framework messages (for example `... не число is not a valid JSON value` instead of `... NaN is not a valid JSON value`). These read as garbled text and made string-based test assertions non-portable across locales. The ICU dependency is also dropped from the native binary.

## 0.2.0

### Minor Changes

- 97cc7f0: `gaffer scaffold` now takes an explicit file path instead of a bare projection name. The bare-name form is gone; users must pass a path that ends in a supported extension (`.js` today).

  ```
  # before
  gaffer scaffold counter

  # after
  gaffer scaffold projections/counter.js
  ```

  The toml key (the projection's name in `gaffer.toml`) defaults to the file's basename without extension. Override with `--name` when the file name and toml key should differ:

  ```
  gaffer scaffold projections/totals.js --name order-totals
  ```

  Same shape on the MCP `scaffold` tool: `path` is now a required field, `name` is optional and defaults to the basename. Path is cwd-relative on the CLI and project-root-relative on MCP; both surfaces normalise backslashes to forward slashes, validate that the path stays inside the project root (including through symlinks), and reject paths without a supported extension or with no filename stem (`.js`, `foo/.js`).

### Patch Changes

- 3b5392c: Documentation links in the README now point at `gaffer.kurrent.io` rather than the `docs.kurrent.io/gaffer/` placeholder.
- d241c58: Drop the half-implemented `enabled` projection key from `gaffer.toml`. The key was honoured by exactly one consumer (MCP's `list_projections` tool, and only as an output tag rather than a filter); every other path - `gaffer dev`, `gaffer info`, `gaffer manifest`, the VS Code lens - ran and listed projections regardless.

  The TOML parser silently ignores unknown keys, so any `enabled = false` left in an existing `gaffer.toml` becomes a no-op rather than an error. MCP `list_projections` no longer tags disabled projections in its output.

- 64d77dc: `gaffer init` now creates only `gaffer.toml`. The empty `.gaffer/` directory and the `.gitignore` entries (`.env`, `.env.*`, `.gaffer/`) were speculative scaffolding: nothing in tree reads the directory, the gitignore patterns presumed too much about the user's project layout (e.g. `.env.*` would have caught `.env.example`), and `.gitignore` was being created even outside a git repo.
- 3707343: `gaffer init` is now non-interactive by default. Previously bare `gaffer init` errored out and pointed at a `--yes` flag the user had no reason to know about; running it now does what `-y` did before. The `--yes` / `-y` flag is parsed but currently a no-op, kept available for forward-compat with the upcoming interactive form.
- a68e3c8: `gaffer manifest` cleanups:
  - The command is now hidden from `gaffer --help`. Its audience is editor extensions and other wrappers that feature-gate their UI against a specific gaffer build, not interactive users.
  - The manifest now walks the full command tree and emits nested commands under composite keys (e.g. `config telemetry status`). Previously only direct children of the root appeared, so the `config` subtree was missing from the output. Non-runnable group commands (e.g. bare `config`) are traversed but not emitted: the manifest lists invocable commands, not navigation nodes.

- 723e35a: `gaffer manifest` now reports `updateAvailable: "x.y.z" | null` alongside `version` and `commands`. The value is sourced from the existing once-per-day update-notifier cache, so manifest fetches add no extra network call. Editor wrappers (the VS Code extension) can surface a one-click update toast without re-checking the npm registry.
- 95af1d2: MCP server gains two read-only introspection tools that mirror the CLI:
  - `get_projection_info` returns the same JSON shape as `gaffer info <name> --json` (parsed structure, sources, partition mode, emit declarations, effective engine version). The projection `name` is optional when the project defines exactly one projection.
  - `get_version` returns the gaffer CLI version string.

  Both are sync, no session state, and don't take a configured KurrentDB connection.

- 723e35a: `gaffer`'s update-check pipeline now separates the stderr notice from the registry refresh.
  - The "Update available" stderr notice is suppressed on machine-readable invocations: `gaffer manifest`, `gaffer lsp`, `gaffer mcp`, or any command run with `--json`. Previously the notice could print onto the sibling stream of a structured stdout payload when stderr was a TTY (e.g. `gaffer manifest | jq`).
  - The once-per-day registry refresh now runs on non-interactive paths too. Previously it was gated on the same TTY check as the notice, so a user invoking gaffer only through an editor wrapper would have a stale-forever cache. The refresh is still skipped under `--no-update-check` and `GAFFER_NO_UPDATE_CHECK=1`.

- 3707343: Restyle the first-mint telemetry disclosure to match the styled card used by `gaffer --help` and the update-available notice, and reword the copy so the lead names what the data is used for (feature prioritisation and bug fixing). `KURRENTDB_TELEMETRY_OPTOUT` and `DO_NOT_TRACK` remain honoured but no longer appear in the banner; the full reference is in `cli/TELEMETRY.md`.
- 09ea79b: Notify when a newer gaffer release is available. On interactive runs the CLI now prints a one-line stderr hint when the cached `latest` version on npm is ahead of the running binary, e.g.

  ```
  gaffer 0.2.0 available (you have 0.1.3). Update with: npm install -g @kurrent/gaffer@latest
  ```

  Notification only - the CLI never self-installs. A background once-per-24h GET against `https://registry.npmjs.org/@kurrent/gaffer/latest` refreshes the cache for the next run; the synchronous read at startup keeps the notice instant. Network failures, non-200s, and malformed responses are silent.

  Suppress with the `--no-update-check` flag or the `GAFFER_NO_UPDATE_CHECK=1` env var. The check skips itself when stderr isn't a TTY, so extension-spawned `gaffer lsp` / `gaffer mcp` / `gaffer manifest --json` invocations and CI runners never see the notice.

## 0.1.3

### Patch Changes

- aeed2b2: Mark the CLI binary as executable in published tarballs.

## 0.1.2

### Patch Changes

- d3d297a: Restore the executable bit on the CLI binary inside each per-platform native package. `actions/upload-artifact@v4` stores artifacts as zip, which drops unix permission bits - 0.1.1 shipped `gaffer` as `0644` so `npx @kurrent/gaffer` failed with `EACCES`. The runtime shared libraries (`.so` / `.dylib` / `.dll`) are unaffected; they load via dlopen and don't need `+x`. Windows resolves executability by `.exe` extension, so the win32 binary is also unaffected.

## 0.1.1

### Patch Changes

- 2675301: Republish the per-platform native packages with their compiled CLI binary and co-located runtime. 0.1.0 shipped those packages empty - `gaffer` exited with `native binary for <platform> not found` on every invocation. Same root cause as the runtime fix: a CI artifact-handling bug. Reinstall `>=0.1.1`.

## 0.1.0

### Minor Changes

- 5b85426: Develop, test, debug, and deploy KurrentDB projections from the command line. Runs projections locally against the same JavaScript engine KurrentDB uses.
  - `gaffer init` - create a new project (`gaffer.toml`, `.gaffer/`, `.gitignore`).
  - `gaffer scaffold <name>` - add a new projection.
  - `gaffer dev` - run a projection against fixtures, an events file, or a live KurrentDB instance.
  - `gaffer info` - inspect projection details.
  - `gaffer config` - manage user-level configuration (telemetry, identity).
  - `gaffer lsp` - Language Server Protocol over stdio for editor integration.
  - `gaffer mcp` - Model Context Protocol server exposing scaffolding, validation, debugging, and the projection API to AI agents.
  - `gaffer version` - print the installed version.
  - Debug Adapter Protocol server for breakpoint debugging, wired up automatically by the [KurrentDB Projections VS Code extension](https://marketplace.visualstudio.com/items?itemName=kurrent-io.gaffer).
