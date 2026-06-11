---
"@kurrent/gaffer-runtime": patch
---

State serialization now enforces a size limit, configurable via `maxStateSizeBytes` (default 16 MiB), raising a `StateSerializationError` when exceeded. This restores KurrentDB's `MaxProjectionStateSize` cap, which gaffer's hand-written session layer never ported. It also bounds the cost of serializing a small acyclic state graph that expands exponentially through shared references.
