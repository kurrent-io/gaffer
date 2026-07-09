---
"@kurrent/gaffer": patch
---

`gaffer status` reports an aborted projection as its own `aborted` runtime state, distinct from a clean `stopped`. An aborted projection was stopped without a final checkpoint, so resuming it reprocesses from the last checkpoint written, re-emitting for an emitting projection.

- The state surfaces everywhere `runtime.state` appears: the status table (with a warning tint), the status detail block, `gaffer status --json`, and the `deploy_status` MCP tool.
- The signal is transient. KurrentDB reports it only while it holds the projection in memory, so it reverts to `stopped` after a server restart, and the absence of `aborted` is not proof of a clean pause.
