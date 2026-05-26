# @kurrent/gaffer

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
