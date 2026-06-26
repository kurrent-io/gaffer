---
"@kurrent/gaffer": patch
---

`gaffer dev --debug` no longer hangs when a Restart arrives as the session is tearing down; the restart returns cleanly during shutdown instead of leaving the debug adapter's read goroutine waiting forever.
