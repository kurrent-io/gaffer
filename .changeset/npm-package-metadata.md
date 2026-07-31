---
"@kurrent/gaffer": patch
"@kurrent/gaffer-runtime": patch
"@kurrent/projections-testing": patch
---

The npm packages declare keywords, so they surface in registry search, and two published-typings corrections: the runtime's error hierarchy and `EventContext` now carry class-level JSDoc, and `feed()`'s hover doc no longer claims thrown errors carry `input` / `normalized` fields (nothing attaches them).
