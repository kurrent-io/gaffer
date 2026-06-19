---
"@kurrent/gaffer": patch
---

`gaffer deploy`'s interactive view no longer drops the last projection's result line when the run finishes. The final row's commit and the program quit now run as one ordered step, so the verdict is always flushed before the view tears down (a single-projection deploy previously showed only the summary).
