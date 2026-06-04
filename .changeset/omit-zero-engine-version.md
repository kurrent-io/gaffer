---
"@kurrent/gaffer": patch
---

`gaffer.toml` handling of `engine_version` has two fixes:

- `gaffer scaffold` (and any command that re-saves the manifest) no longer writes `engine_version = 0` for projections with no engine version set. Previously the line was stamped on save, including onto existing projections.
- An explicit `engine_version = 0` is now rejected with "must be 1 or 2, got 0" instead of being silently treated as unset.
