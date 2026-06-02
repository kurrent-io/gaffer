---
"@kurrent/gaffer-runtime": patch
"@kurrent/projections-testing": patch
---

`quirk.log.multiParam` now also surfaces as a runtime diagnostic on `FeedResult.diagnostics` (and the testing library's processed `step.diagnostics`), fired at each multi-argument `log()` call as it runs. Previously this quirk was reported only at compile time. It joins the biState string-slot quirks already carried there, so a test can assert it fired without inspecting the projection's compile-time diagnostics. It reports once per call rather than once per event.
