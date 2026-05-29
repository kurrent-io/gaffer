---
"@kurrent/gaffer": patch
---

`gaffer dev` now surfaces runtime quirks at the point they fire while processing an event, such as a `biState` string slot being JSON-quoted on persistence or a multi-argument `log()` call. In text output each quirk prints inline, interleaved with the handler's `log()` lines and emits in the order they happened, so stepping through a projection shows the warning as you hit the line. The JSON result line still carries a `diagnostics` array, and the run summary tallies every distinct quirk, compile-time and runtime alike. A `gaffer/stepWarning` DAP event also fires live per quirk, so editor integrations can attach the warning to the step.
