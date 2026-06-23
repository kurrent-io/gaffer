---
"@kurrent/gaffer": patch
---

A per-projection config error (e.g. `track_emitted_streams` with `engine_version 2`) no longer blocks every command. Previously one misconfigured projection failed `gaffer.toml` loading outright, so `gaffer info <good-projection>` died on an *unrelated* projection's error. Now config validation splits into structural checks (environments, duplicate names) that stay fatal, and per-projection checks that are deferred; a bad projection only blocks operations on itself. The inspection commands (`status`, `diff`, `info`) show it as `invalid` through one shared rendering; `deploy` refuses just that one; `recreate` and the operate verbs fail only when you name it. Mirrors the per-projection degradation already used for compile errors.
