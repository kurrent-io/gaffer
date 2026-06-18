---
"@kurrent/gaffer": patch
---

`gaffer diff <projection>` compares a projection's local definition against what's deployed on KurrentDB and reports its state: in sync, drifted, not deployed, or untracked.

When the query differs, the source is shown in an external diff viewer. By default this is `git diff --no-index`; set `GAFFER_EXTERNAL_DIFF` to override. Pass `--json` for machine-readable output.
