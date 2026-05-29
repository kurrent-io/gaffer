---
"@kurrent/gaffer": patch
---

`gaffer dev` now surfaces runtime quirks that fire while processing an event, such as a `biState` string slot being JSON-quoted on persistence. Each fired quirk shows per-event (a `quirk:` line in text output, a `diagnostics` array on the JSON result line) and is counted in the run summary. A `gaffer/stepWarning` DAP event is also emitted so editor integrations can attach the warning to the step.
