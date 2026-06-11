# @kurrent/projections-testing

## 0.2.1

### Patch Changes

- 401093e: Errors from `partitionBy`, `$init`, `$initShared`, and `$created` now surface as structured projection errors with event context, like errors from event handlers. Previously they escaped as raw engine exceptions that the bindings reported as a generic "unexpected" error with no stream or sequence context, and a `partitionBy` timeout could not be caught by type. `getPartitionKey` wraps the same way.
- 12dbafc: `createProjection` now forwards `databaseConfig.maxStateSizeBytes` to the session, letting tests configure the serialized-state size limit (default 16 MiB).
- Updated dependencies [cc72aae]
- Updated dependencies [c1e2d9b]
- Updated dependencies [eb5573c]
- Updated dependencies [401093e]
- Updated dependencies [2f31371]
- Updated dependencies [65bc7f1]
- Updated dependencies [21b0bad]
- Updated dependencies [3a3a921]
- Updated dependencies [e01399d]
- Updated dependencies [12dbafc]
- Updated dependencies [3a3a921]
- Updated dependencies [7b1d552]
  - @kurrent/gaffer-runtime@0.3.0

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

- 7fff86a: `feed()` and `run()` now skip events whose stream the projection's declared source does not subscribe to, matching what real KurrentDB would deliver. A `fromStream("s-1")` projection fed an event on `s-2` previously processed it; it now returns `{ status: "skipped", reason: "wrong-stream" }`. `fromStreams`, `fromCategory` (by stream prefix), and `fromAll` (everything passes) follow the same rule. This closes a footgun where unit tests passed against events the projection would never see in production.
- e9dfaff: The quirks-selecting option and the quirk registry are renamed to retire the misleading "DB version" / "bug" framing.
  - **`dbVersion` is now `quirksVersion`** across the runtime, the JS bindings (`SessionOptions`), and the testing library (`ProjectionOptions`). The value is unchanged: a `MAJOR.MINOR.PATCH` string, where unset still reproduces every known quirk and a set version turns off quirks fixed upstream as of it. Only the key moves. `dbVersion` read as passive info when it actively selects which quirks to emulate, and it collided with `engineVersion`.
  - **`knownBugs()` is now `knownQuirks()`**, and **`KnownBug` is now `KnownQuirk`**, in the JS and Go bindings. Most registry entries are deliberate KurrentDB quirks gaffer reproduces, not bugs to report upstream.
  - **CLI**: the `gaffer.toml` key `db_version` is now `quirks_version`, the env var `GAFFER_DB_VERSION` is now `GAFFER_QUIRKS_VERSION`, and the MCP resource `gaffer://docs/db-version-bugs` is now `gaffer://docs/quirks`. The connected-server-version telemetry (the `db_version` event property) is unaffected, since it genuinely reports the connected DB version.

  No deprecation period: pre-1.0, hard break. An old `dbVersion` or `db_version` key is silently ignored rather than rejected, so update existing call sites and `gaffer.toml` files.

- 223e9ab: The `TestEvent` schema now rejects inputs KurrentDB could never deliver to a handler: `eventType` and `streamId` must be non-empty strings, and `sequenceNumber` must be a non-negative integer. Previously a unit test could pass against events the projection would never see in production, such as a negative sequence number or an empty stream id.

### Patch Changes

- 9411111: The runtime and testing library now report three previously cryptic errors with friendlier messages:
  - `foreachStream()` on a `fromStream()` or `fromStreams()` projection now fails with "foreachStream() is only supported with fromAll() and fromCategory()", instead of a raw "Property 'foreachStream' of object is not a function" engine error.
  - A second `options()` call now produces a compile-time warning, since only the last call takes effect and the earlier ones are discarded silently.
  - The testing library now names which event shape was attempted and which field is wrong when a fed event matches none, instead of valibot's cryptic `Expected Object but received Object`.

- 627dd02: `quirk.log.multiParam` now also surfaces as a runtime diagnostic on `FeedResult.diagnostics` (and the testing library's processed `step.diagnostics`), fired at each multi-argument `log()` call as it runs. Previously this quirk was reported only at compile time. It joins the biState string-slot quirks already carried there, so a test can assert it fired without inspecting the projection's compile-time diagnostics. It reports once per call rather than once per event.
- b217c5e: The runtime now builds with `InvariantGlobalization` enabled, so error messages stay English regardless of the host machine's locale. Previously a non-English-preference machine produced partially-translated framework messages (for example `... не число is not a valid JSON value` instead of `... NaN is not a valid JSON value`). These read as garbled text and made string-based test assertions non-portable across locales. The ICU dependency is also dropped from the native binary.
- fb61e4c: `FeedResult` (and the testing library's processed `step`) now carries a `diagnostics` array of quirks that fired while processing the event. It reuses the compile-time `Diagnostic` shape but with a null range, since runtime quirks are value-dependent and have no source location.

  The motivating runtime quirk is `quirk.serialize.rawString`: a bare string state that isn't valid JSON is persisted un-encoded (e.g. `hello` rather than `"hello"`), so the projection faults on reload when `JSON.parse` runs on the stored value. Fixed in KurrentDB 26.2.0 by always JSON-encoding string state.

  The testing library also adds `getStateRaw(partition?)` and `step.stateRaw`, returning the persisted state JSON string before `JSON.parse`, so a test can assert the exact persisted string a quirk produces (which the parsed `state` hides).

- Updated dependencies [cf26d46]
- Updated dependencies [9f9722a]
- Updated dependencies [9411111]
- Updated dependencies [627dd02]
- Updated dependencies [47cfe96]
- Updated dependencies [e9dfaff]
- Updated dependencies [b217c5e]
- Updated dependencies [fb61e4c]
  - @kurrent/gaffer-runtime@0.2.0

## 0.1.2

### Patch Changes

- 3b5392c: Documentation links in the README now point at `gaffer.kurrent.io` rather than the `docs.kurrent.io/gaffer/` placeholder.
- Updated dependencies [3b5392c]
  - @kurrent/gaffer-runtime@0.1.2

## 0.1.1

### Patch Changes

- 2675301: Republish to track `@kurrent/gaffer-runtime@^0.1.1`. The runtime's native binary was missing in 0.1.0, so any test using this library at 0.1.0 failed to load the projection engine.
- Updated dependencies [2675301]
  - @kurrent/gaffer-runtime@0.1.1

## 0.1.0

### Minor Changes

- 5b85426: Test KurrentDB projections locally with any test runner (vitest, jest, mocha). Wraps `@kurrent/gaffer-runtime` to execute projections against test events with the same behaviour as a real KurrentDB instance.
  - `createProjection<TState>(source, options?)` - prepare a projection for repeated runs. Compiles lazily on first `validate` / `run` / `test`.
  - `projection.validate()` - compile and return the projection's source definition.
  - `projection.run(events)` - replay events from an array, async iterable, or live `KurrentDBClient` subscription. Yields a `StepResult` per event.
  - `projection.test()` - interactive session: feed events one at a time, query state by partition, inspect emitted events and logs.
  - `systemEvents` helpers for constructing KurrentDB system events (e.g. `streamDeleted`).
  - Typed `ProjectionError` subclasses propagated from the runtime with structured fields and formatted messages.

### Patch Changes

- Updated dependencies [5b85426]
  - @kurrent/gaffer-runtime@0.1.0
