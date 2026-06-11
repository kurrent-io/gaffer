---
"@kurrent/gaffer-runtime": patch
---

Sessions now release their native handle and koffi callback slots via a `FinalizationRegistry` if garbage-collected without `dispose()`, guarding long-running processes against koffi's hard callback-slot cap.
