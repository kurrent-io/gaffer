---
"@kurrent/gaffer-runtime": patch
---

A bi-state projection whose handler returns a non-array (instead of the `[state, sharedState]` pair) now emits a `quirk.biState.nonArrayReturn` diagnostic. KurrentDB persists the malformed value and then wedges the partition on the next event; gaffer reproduces that and surfaces the diagnostic so the cause is visible instead of an unexplained failure.
