---
"@kurrent/gaffer-runtime": patch
---

A faulted projection session (one whose `emit` or `log` callback threw) now refuses partition resolution, state initialization, and the result/transform path, not just event handling. Previously those entry points still re-ran user code (`partitionBy`, `$init`, `$initShared`, `transformBy`/`filterBy`) on a session already marked faulted, which could re-invoke the failing callback instead of failing fast.
