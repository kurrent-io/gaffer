---
"@kurrent/gaffer": patch
---

`gaffer dev` text output now prints a handler's `log()` lines and emitted events under their own event header, in the order they happened, instead of before the header. The header is deferred until the result is known (so skipped events can be dropped silently), but logs and emits produced during processing now flush that header first.
