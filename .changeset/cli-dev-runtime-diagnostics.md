---
"@kurrent/gaffer": patch
---

`gaffer dev` now surfaces runtime quirks encountered while processing an event, such as a `biState` string slot being JSON-quoted on persistence. Each one shows per-event (a `[warning]` line in text output, a `diagnostics` array on the JSON result line) and is tallied in the run summary. A `gaffer/stepWarning` DAP event is also emitted so editor integrations can attach the warning to the step.
