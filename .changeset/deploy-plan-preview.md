---
"@kurrent/gaffer": patch
---

`gaffer deploy`'s plan preview now lists each projection, not just totals. Every projection that would change shows a verdict - `create`, `update`, `rebuild`, `refused`, or `failed` - and a dimmed detail column carrying the refusal reason or the failure error in full. In-sync projections stay a count only, so unchanged ones don't drown the signal.
