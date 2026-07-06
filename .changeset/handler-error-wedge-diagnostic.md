---
"@kurrent/gaffer": patch
"@kurrent/gaffer-runtime": patch
---

Event-processing errors under `engine_version 2` now carry the `quirk.handlerError.wedgesOnV2` (error severity) diagnostic. On deployed V2, an exception thrown while processing an event never faults the projection. It wedges silently: `status` stays `Running` while processing and persistence have stopped, and nothing is logged. Gaffer keeps faulting the event locally, which is the behaviour V2 should have, and the diagnostic rides the error to explain the divergence. It fires for any event-processing throw (handler, state load, `$created`, `$deleted`, state serialization, timeout); a throwing `partitionBy` is exempt because the server computes partition keys on its read loop, which faults properly.
