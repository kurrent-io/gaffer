---
"@kurrent/projections-testing": patch
---

Republish to track `@kurrent/gaffer-runtime@^0.1.1`. The runtime's native binary was missing in 0.1.0, so any test using this library at 0.1.0 failed to load the projection engine.
