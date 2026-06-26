---
"@kurrent/gaffer": patch
---

`gaffer deploy` now aligns its per-projection verdict column by the display width of projection names. Names containing multi-byte characters (CJK, accented letters) previously over-padded the column and pushed the verdicts out of alignment.
