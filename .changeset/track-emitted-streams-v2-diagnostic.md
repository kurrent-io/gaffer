---
"@kurrent/gaffer": patch
"@kurrent/gaffer-runtime": patch
---

`track_emitted_streams` with `engine_version 2` is now reported as a diagnostic rather than a config-load error. The runtime emits `quirk.trackEmittedStreams.unsupportedOnV2` (error severity) off the resolved definition, whether the flag comes from `gaffer.toml` or `options({ trackEmittedStreams: true })` in the source. This matches how the other V2 incompatibilities (bi-state, `outputState`) already surface.

`gaffer info`, `gaffer dev`, and `gaffer diff` now compile such a projection and show its full analysis plus the flag, instead of failing with a bare config error. `gaffer deploy` and `gaffer recreate` still refuse it at preflight (recreate before deleting anything), and the MCP `validate` tool reports it invalid with the diagnostic. The projection session no longer throws on the combination.
