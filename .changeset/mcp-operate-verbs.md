---
"@kurrent/gaffer": patch
---

The MCP server gains the operate verbs, so an assistant can manage a deployed projection's lifecycle with a human in the loop:

- **`deploy_pause`** / **`deploy_resume`** / **`deploy_abort`** mirror `gaffer disable` / `enable` / `disable --abort`.
- **`deploy_recreate`** rebuilds from local config like `gaffer recreate`, gated on the compile and diagnostics preflight, and stamps `operation: recreate` in the deploy ledger.
- **`deploy_rollback`** redeploys a prior version by content hash from `deploy_history`, like `gaffer rollback`, and stamps `operation: rollback`.
- **`deploy_delete`** mirrors `gaffer delete`, including `deleteEmitted`.

Writes against a server that reports itself as production require a confirmation answered through the MCP client (elicitation); the assistant cannot answer it. Recreate and delete destroy state with no undo, so they ask every time, production or not. A client without elicitation support cannot perform gated writes; the refusal names the CLI command to run instead.
