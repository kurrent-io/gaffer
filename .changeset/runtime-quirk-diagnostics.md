---
"@kurrent/gaffer-runtime": patch
"@kurrent/projections-testing": patch
---

`FeedResult` (and the testing library's processed `step`) now carries a `diagnostics` array of quirks that fired while processing the event. It reuses the compile-time `Diagnostic` shape but with a null range, since runtime quirks are value-dependent and have no source location.

The motivating runtime quirk is `quirk.serialize.rawString`: a bare string state that isn't valid JSON is persisted un-encoded (e.g. `hello` rather than `"hello"`), so the projection faults on reload when `JSON.parse` runs on the stored value. Fixed in KurrentDB 26.2.0 by always JSON-encoding string state.

The testing library also adds `getStateRaw(partition?)` and `step.stateRaw`, returning the persisted state JSON string before `JSON.parse`, so a test can assert the exact persisted string a quirk produces (which the parsed `state` hides).
