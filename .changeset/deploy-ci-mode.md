---
"@kurrent/gaffer": patch
---

`gaffer deploy` gains `--dry-run` and a stable exit-code contract for CI. `--dry-run` shows the plan and applies nothing. The exit code is now `0` succeeded or nothing to do, `1` an error, and `2` changes are pending (`--dry-run` only). A new `3` means refused by a guardrail: confirmation was needed but there was no terminal or `--yes`, or `--no-validate` was used against production.

`--dry-run` reuses the same per-projection plan output as a confirmed deploy, and its `--json` is the same array shape, so a pipeline can branch on exit `2` or parse the would-be outcomes. The guardrail exit code `3` also applies to `recreate` and the operate verbs when they can't confirm non-interactively. A production `--no-validate` refusal now prints its reason instead of exiting silently.
