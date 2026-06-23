---
"@kurrent/gaffer": patch
---

A per-projection config error (e.g. `track_emitted_streams` with `engine_version 2`) no longer blocks every command. Previously one misconfigured projection failed `gaffer.toml` loading outright, so `gaffer info <good-projection>` died on an *unrelated* projection's error. Now config validation splits into structural checks (environments, duplicate names) that stay fatal, and per-projection checks that are deferred. A bad projection only blocks operations on itself. `gaffer status` and `diff` flag it as `invalid`, `deploy` refuses just that one, and single-projection commands fail only when you name the bad projection. Mirrors the per-projection degradation already used for compile errors.
