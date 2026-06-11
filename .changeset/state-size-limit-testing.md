---
"@kurrent/projections-testing": patch
---

`createProjection` now forwards `databaseConfig.maxStateSizeBytes` to the session, letting tests configure the serialized-state size limit (default 16 MiB).
