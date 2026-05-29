---
"@kurrent/gaffer-runtime": patch
"@kurrent/projections-testing": patch
---

`FeedResult` (and the testing library's processed `step`) now carries a `diagnostics` array of quirks that fired while processing the event, reusing the compile-time `Diagnostic` shape but with a null range since they are value-dependent and have no source location.

The first quirks surfaced are biState string slots: a raw string written to a state slot is JSON-quoted on persistence, so `"hello"` is stored as `"\"hello\""`. This is now two registry entries - `compat.biState.stringSlot` for the main slot and `compat.biState.sharedStringSlot` for shared state - because the upstream fix only addresses the main slot.

The testing library also adds `getStateRaw(partition?)` and `step.stateRaw`, returning the persisted state JSON string before `JSON.parse`, so a test can assert the double-quoted value a quirk produces (which the parsed `state` hides).
