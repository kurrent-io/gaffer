---
"@kurrent/gaffer": patch
---

`gaffer deploy` and `gaffer status` warn when the target node's live engine settings diverge from a declared `[database_config]`: one line per differing knob, read from the node's options endpoint. Fixtures and local runs assumed the declared values, so a server enforcing a different `max_state_size` or timeout is visible before it bites. The check is advisory: a server that doesn't expose its options, or refuses the read, skips it silently, and a non-positive `max_state_size` declares the engine default rather than a value.

`gaffer status --json` now emits an object: a `projections` array (the previous per-projection entries) plus a `configDrift` array of `{"knob", "server", "local"}` when the target diverges. Machine consumers see the divergence without a second call. `gaffer deploy --json` is unchanged; its warning prints on stderr, keeping the stdout payload clean while CI logs still show it.
