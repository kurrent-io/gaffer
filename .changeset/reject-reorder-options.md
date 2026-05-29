---
"@kurrent/gaffer-runtime": patch
"@kurrent/gaffer": patch
---

Setting `reorderEvents` or `processingLag` on a projection whose source is not `fromStreams()` now produces a compile-time error diagnostic. These options only apply to `fromStreams([])`: KurrentDB rejects `reorderEvents` on other sources at subscription time, and `processingLag` has no effect without it. Gaffer previously accepted both on any source and silently ignored them.
