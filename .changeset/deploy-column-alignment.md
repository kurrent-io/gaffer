---
"@kurrent/gaffer": patch
---

`gaffer deploy` now measures its per-projection verdict column by character count instead of byte length, so projection names with multi-byte characters such as accented letters no longer over-pad the column and misalign the verdicts.
