---
"@kurrent/gaffer": patch
---

The deploy and projection-management commands now report the same anonymous usage telemetry as the rest of the CLI. `gaffer deploy`, `status`, `diff`, `history`, `rollback`, `recreate`, `enable`, `disable`, and `delete` each record which command ran and how it finished, including when a guardrail refuses the run. The mutating commands also record whether the target is production. `deploy` and `recreate` record whether validation was skipped, `deploy` whether the run was a dry-run, and `history` whether a rollback was applied from the timeline. Only buckets, booleans, and outcomes are collected. Projection names, connection strings, and content hashes never are. Opt out with `gaffer config telemetry off` or `telemetry = false` in `gaffer.toml`.
