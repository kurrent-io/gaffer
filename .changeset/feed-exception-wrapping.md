---
"@kurrent/gaffer-runtime": patch
"@kurrent/projections-testing": patch
---

Errors from `partitionBy`, `$init`, `$initShared`, and `$created` now surface as structured projection errors with event context, like errors from event handlers. Previously they escaped as raw engine exceptions that the bindings reported as a generic "unexpected" error with no stream or sequence context, and a `partitionBy` timeout could not be caught by type. `getPartitionKey` wraps the same way.
