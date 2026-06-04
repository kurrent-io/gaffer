# @kurrent/gaffer-runtime

## 0.2.0

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
- 9411111: The runtime and testing library now report three previously cryptic errors with friendlier messages:
  - `foreachStream()` on a `fromStream()` or `fromStreams()` projection now fails with "foreachStream() is only supported with fromAll() and fromCategory()", instead of a raw "Property 'foreachStream' of object is not a function" engine error.
  - A second `options()` call now produces a compile-time warning, since only the last call takes effect and the earlier ones are discarded silently.
  - The testing library now names which event shape was attempted and which field is wrong when a fed event matches none, instead of valibot's cryptic `Expected Object but received Object`.

- 627dd02: `quirk.log.multiParam` now also surfaces as a runtime diagnostic on `FeedResult.diagnostics` (and the testing library's processed `step.diagnostics`), fired at each multi-argument `log()` call as it runs. Previously this quirk was reported only at compile time. It joins the biState string-slot quirks already carried there, so a test can assert it fired without inspecting the projection's compile-time diagnostics. It reports once per call rather than once per event.
- 47cfe96: Setting `reorderEvents` or `processingLag` on a projection whose source is not `fromStreams()` now produces a compile-time error diagnostic. These options only apply to `fromStreams([])`: KurrentDB rejects `reorderEvents` on other sources at subscription time, and `processingLag` has no effect without it. Gaffer previously accepted both on any source and silently ignored them.
- b217c5e: The runtime now builds with `InvariantGlobalization` enabled, so error messages stay English regardless of the host machine's locale. Previously a non-English-preference machine produced partially-translated framework messages (for example `... не число is not a valid JSON value` instead of `... NaN is not a valid JSON value`). These read as garbled text and made string-based test assertions non-portable across locales. The ICU dependency is also dropped from the native binary.
- fb61e4c: `FeedResult` (and the testing library's processed `step`) now carries a `diagnostics` array of quirks that fired while processing the event. It reuses the compile-time `Diagnostic` shape but with a null range, since runtime quirks are value-dependent and have no source location.

  The motivating runtime quirk is `quirk.serialize.rawString`: a bare string state that isn't valid JSON is persisted un-encoded (e.g. `hello` rather than `"hello"`), so the projection faults on reload when `JSON.parse` runs on the stored value. Fixed in KurrentDB 26.2.0 by always JSON-encoding string state.

  The testing library also adds `getStateRaw(partition?)` and `step.stateRaw`, returning the persisted state JSON string before `JSON.parse`, so a test can assert the exact persisted string a quirk produces (which the parsed `state` hides).

## 0.1.2

### Patch Changes

- 3b5392c: Documentation links in the README now point at `gaffer.kurrent.io` rather than the `docs.kurrent.io/gaffer/` placeholder.

## 0.1.1

### Patch Changes

- 2675301: Republish the per-platform native packages with their compiled `gaffer.so` / `.dylib` / `.dll`. 0.1.0 shipped those packages empty due to a CI workflow bug (`upload-artifact@v4` strips directory paths for single-file uploads, so the download step at publish time saw colliding bare-named files at the workspace root instead of files in their per-platform package dirs). Installing 0.1.0 left koffi unable to load the runtime. Reinstall `>=0.1.1` to pick up the fix.

## 0.1.0

### Minor Changes

- 5b85426: Low-level Node.js bindings for the gaffer projection runtime. Runs the same JavaScript engine KurrentDB uses for server-side projections, wrapping the NativeAOT shared library.
  - `ProjectionSession` - feed events, query state by partition, observe emits / logs / state changes, dispose.
  - `knownBugs()` - the runtime's list of known engine bugs by version, surfaced as actionable warnings in editor tooling.
  - Typed `ProjectionError` subclasses (handler errors, malformed events, compilation / execution timeouts, serialization failures) with structured fields.
  - Underpins [`@kurrent/projections-testing`](https://www.npmjs.com/package/@kurrent/projections-testing) and the [KurrentDB Projections VS Code extension](https://marketplace.visualstudio.com/items?itemName=kurrent-io.gaffer).
