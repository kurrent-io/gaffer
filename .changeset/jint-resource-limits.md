---
"@kurrent/gaffer-runtime": patch
---

The projection sandbox now bounds script recursion depth and cumulative per-event allocation. A runaway or hostile projection that previously crashed the host process (uncatchable stack overflow) or exhausted its memory now raises a catchable `ProjectionHandlerError` (or `ProjectionTransformError`) instead. Deeply nested event JSON was already rejected as malformed; this closes the equivalent gap for projection code.
