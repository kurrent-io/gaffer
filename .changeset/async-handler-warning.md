---
"@kurrent/gaffer-runtime": patch
"@kurrent/gaffer": patch
---

Projection handlers that use `async` or return a `Promise` now produce a compile-time warning. The projection engine is synchronous (no event loop), so it serializes the returned `Promise` as the state instead of awaiting it, leaving the state as `{}`. This matches KurrentDB but is surprising when authoring tests in an async-capable JS runtime, so gaffer flags it. The `Promise` check is a literal-syntax heuristic (`new Promise(...)`, `Promise.resolve(...)`, and similar).
