---
"@kurrent/gaffer-runtime": patch
---

Clearing a bi-state projection's shared state now sticks. Previously, when a handler set the shared slot to `null`, the assignment was skipped and the prior value was reloaded on the next event, silently resurrecting shared state the handler had cleared. The shared slot now mirrors the partition slot and is assigned unconditionally.
