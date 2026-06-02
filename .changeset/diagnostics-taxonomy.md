---
"@kurrent/gaffer-runtime": minor
"@kurrent/projections-testing": minor
"@kurrent/gaffer": minor
---

Diagnostic codes now use one `quirk.*` / `usage.*` taxonomy. Every diagnostic has a three-segment code `<class>.<subject>.<detail>`, where `quirk.*` reproduces a KurrentDB engine bug and `usage.*` flags something about your own projection.

This is a breaking rename of the diagnostic codes surfaced on `FeedResult.diagnostics`, `ProjectionInfo.diagnostics`, the testing library's `step.diagnostics`, and the CLI/MCP output:

- `compat.linkStreamTo.outOfBoundsParameters` → `quirk.linkStreamTo.outOfBoundsParameters`
- `compat.log.multiParam` → `quirk.log.multiParam`
- `compat.event.bodyCast` → `quirk.event.bodyCast`
- `compat.biState.stringSlot` → `quirk.biState.stringSlot`
- `compat.biState.sharedStringSlot` → `quirk.biState.sharedStringSlot`
- `compat.serialize.nonFinite` → `quirk.serialize.nonFinite`
- `compat.transforms.notInvoked` → `usage.transforms.notInvoked`
- `compat.outputState.unconditional` → `usage.outputState.unconditional`
- `deprecated.linkStreamTo` → `usage.linkStreamTo.deprecated` (now Information, was Warning)
- `options.duplicate` → `usage.options.duplicate`
- `handler.async` → `usage.handler.async`
- `handler.promise` → `usage.handler.promise`

Other changes in this release:

- **Severity is Error / Warning / Information only.** The unused `Hint` level (LSP 4) is dropped from `DiagnosticSeverity`. Severity follows a per-firing rubric: Error when there is no correct form (it throws or is unsupported), Warning when it runs but produces a wrong result, Information when it works but is noteworthy.
- **`reorderEvents` is engine-version aware.** Under `engine_version 1`, an invalid reordering config (not `fromStreams()` with 2+ streams, or `processingLag` below 50ms) is rejected at session create, matching KurrentDB's `ReaderStrategy`. Under `engine_version 2` it has no effect and surfaces as a `usage.reorderEvents.noEffectOnV2` warning rather than the old unconditional error. This replaces the `options.fromStreamsOnly` diagnostic.
- **Throwing quirks now also raise a diagnostic.** A quirk that throws (e.g. `quirk.event.bodyCast`, `quirk.serialize.nonFinite`) exposes a `diagnostics` array on the thrown error, surfaced on the Go error types and the JS `ProjectionError` and propagated through the testing library. The array carries the quirk that threw plus any that fired earlier in the same event, so it is the complete set where `compatCode` is just the throwing quirk's code. Errors are also enriched with `compatDescription` and `compatFixedIn`.
- **Quirk-catalogue exports are removed.** The catalogue is no longer exported over FFI: `knownQuirks()` (and the `KnownQuirk` type) is gone from the JS runtime binding, and `KnownQuirks()` / `KnownQuirk` / `DiagnosticSeverityHint` are gone from the Go binding. Assert on `step.diagnostics` (the data plane) instead.
