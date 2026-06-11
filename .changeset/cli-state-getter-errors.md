---
"@kurrent/gaffer": patch
---

`gaffer dev`, the MCP tools (`get_state`, `run`, `debug`), and the DAP `gaffer/partitionState` request now surface state-getter errors instead of silently returning partial or empty state. A throwing V1 `transformBy`/`filterBy` during state collection previously looked identical to an absent value. `get_state` now returns a tool error, `run`/`debug` results carry a `stateError` field when state collection fails, the DAP partition-state request returns an error response, and `gaffer dev` prints a `warning: reading projection state: ...` line while still showing the summary.
