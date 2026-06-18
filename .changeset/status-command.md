---
"@kurrent/gaffer": patch
---

`gaffer status` shows the runtime state of projections on an environment and how they compare to local config: running, stopped or faulted, progress, and whether each is in sync, drifted, not deployed, or untracked.

With no argument it lists every local and deployed projection as a table; name a projection for its detail. Pass `--json` for machine-readable output.
