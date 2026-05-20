---
"@kurrent/gaffer": patch
---

`gaffer manifest` now reports `updateAvailable: "x.y.z" | null` alongside `version` and `commands`. The value is sourced from the existing once-per-day update-notifier cache - no extra network call per manifest fetch - and lets editor wrappers (the VS Code extension) surface a one-click update toast without re-checking the npm registry.
