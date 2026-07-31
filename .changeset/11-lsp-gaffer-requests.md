---
"@kurrent/gaffer": patch
---

The language server now serves the projection actions editors need over its warm per-environment connection, so an editor no longer spawns a `gaffer` process per action, and advertises which of them it supports.

- **`gaffer/operateProjection`** runs an operate verb (pause / resume / abort / delete) on a projection.
- **`gaffer/diffVersions`** diffs any two versions of a projection - each a content hash, `deployed`, or `local`. It uses the same builder as `gaffer diff --left --right`, so the result matches the CLI's `--json` shape. `gaffer diff` itself is unchanged.

Both are gated on the same `statusLens` initialization option as the deployment-status lenses.

**The server lists the `gaffer/*` requests it serves** in its initialize result, under `capabilities.experimental.gaffer.methods`. LSP has standard capability fields for standard features but no slot for a server's own requests, so an editor extension driving an older `gaffer` previously had no way to discover support except to send a request and read `MethodNotFound` off the failure - by which point the user had already clicked something. Editors can now hide an action their CLI can't run.
