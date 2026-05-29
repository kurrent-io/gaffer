---
"gaffer-vscode": patch
---

The debug Step panel now shows the runtime quirks that fired while processing an event. Each `gaffer/stepWarning` from the CLI appears as a warning node under the step, inline with the handler's logs and emitted events in the order they happened. Stepping through a projection surfaces a quirk as you hit it. Runtime quirks stay off the Problems panel by design: they are value-dependent and have no source range, so they belong on the execution surface rather than the static-analysis one.

The Status view also tallies the distinct runtime quirks seen so far, alongside the processed and error counts.
