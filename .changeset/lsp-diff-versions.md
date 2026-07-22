---
"@kurrent/gaffer": patch
---

The language server serves a new `gaffer/diffVersions` request: a diff between any two versions of a projection - each a content hash, `deployed`, or `local` - over the warm per-env connection, for the VS Code history viewer's per-entry diffs. It uses the same builder as `gaffer diff --left --right`, so the result matches the CLI's `--json` shape. `gaffer diff` itself is unchanged.
