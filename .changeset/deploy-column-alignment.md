---
"@kurrent/gaffer": patch
---

`gaffer deploy` now measures its per-projection verdict column by terminal display width, so projection names with multi-byte or full-width characters no longer over-pad the column and misalign the verdicts.
