---
"@kurrent/gaffer": patch
---

`gaffer dev --json` now emits a `run_error` message when a run ends on a connection failure (a dropped subscription or a failed connect), carrying the reason. Previously this only reached the output as plain stderr text. The VS Code extension uses it to show the failure as a notification and reflect it in the status panel, instead of a silent failure or a bare exit code.
