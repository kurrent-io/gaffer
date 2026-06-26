---
"@kurrent/gaffer": patch
---

Projection errors that reach the CLI wrapped in another error now keep their original error code and diagnostics. Previously a wrapped feed error was classified as `unexpected-error` and its diagnostics were dropped.
