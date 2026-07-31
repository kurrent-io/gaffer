---
"@kurrent/gaffer": patch
"@kurrent/gaffer-runtime": patch
"@kurrent/projections-testing": patch
---

The published packages declare keywords, so they surface in registry search.

Two corrections to the published typings land alongside:

- `@kurrent/gaffer-runtime`'s error hierarchy and `EventContext` now carry class-level JSDoc.
- `@kurrent/projections-testing`'s `feed()` hover doc no longer claims thrown errors carry `input` and `normalized` fields. Nothing attaches them.
