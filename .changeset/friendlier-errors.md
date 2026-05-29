---
"@kurrent/gaffer-runtime": patch
"@kurrent/gaffer": patch
"@kurrent/projections-testing": patch
---

The runtime and testing library now report three previously cryptic errors with friendlier messages:

- `foreachStream()` on a `fromStream()` or `fromStreams()` projection now fails with "foreachStream() is only supported with fromAll() and fromCategory()", instead of a raw "Property 'foreachStream' of object is not a function" engine error.
- A second `options()` call now produces a compile-time warning, since only the last call takes effect and the earlier ones are discarded silently.
- The testing library now names which event shape was attempted and which field is wrong when a fed event matches none, instead of valibot's cryptic `Expected Object but received Object`.
