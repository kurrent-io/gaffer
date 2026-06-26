---
"@kurrent/gaffer": patch
---

`gaffer dev --json` now exits non-zero if it fails to write its output stream (for example a broken pipe to the editor), instead of silently finishing with a truncated stream.
