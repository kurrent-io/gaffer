---
"@kurrent/gaffer": patch
---

`gaffer status` and `gaffer diff` no longer abort when a local projection fails to compile. A compile error is now a per-projection condition, not a whole-command failure: `gaffer status` shows the broken projection as `invalid` and still renders the rest of the table with their real runtime state and drift, and `gaffer diff` still shows the source diff, engine version, and track-emitted-streams (marking `emit` unknown, since deriving it needs a successful compile). Both exit 0; the compile error is shown so you know what to fix.
