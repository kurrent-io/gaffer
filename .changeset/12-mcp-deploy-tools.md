---
"@kurrent/gaffer": patch
---

The MCP server gains the deploy and projection-management tools, so an assistant can read a deployment and change it with a human in the loop.

**Read-only**, mirroring the CLI's machine output:

- **`deploy_status`** shows each projection's runtime state and drift verdict on an environment, plus any `[database_config]` divergence, like `gaffer status --json`.
- **`deploy_plan`** previews what a deploy would change without applying anything, like `gaffer deploy --dry-run --json`.
- **`deploy_history`** reads a projection's per-deploy audit log with paging, like `gaffer history --json`.

**Writes:**

- **`deploy_apply`** deploys projections from `gaffer.toml` like `gaffer deploy`, with the same all-or-nothing compile and diagnostics preflight (no validation bypass), the same per-item results as `gaffer deploy --json`, and every write stamped `operation: deploy` in the ledger.
- **`deploy_pause`** / **`deploy_resume`** / **`deploy_abort`** mirror `gaffer disable` / `enable` / `disable --abort`.
- **`deploy_recreate`** rebuilds from local config like `gaffer recreate`, gated on the compile and diagnostics preflight, and stamps `operation: recreate`.
- **`deploy_rollback`** redeploys a prior version by content hash from `deploy_history`, like `gaffer rollback`, and stamps `operation: rollback`.
- **`deploy_delete`** mirrors `gaffer delete`, including `deleteEmitted`.

All tools accept an `env` argument and default to the default environment; `deploy_status` and `deploy_plan` echo the resolved target and production flag.

**Confirmation is answered through the MCP client** (elicitation) and the assistant cannot answer it. A write against a production target, whether the server reports itself as production or the env sets `production = true`, requires one: the prompt front-loads `PRODUCTION [env.<name>]:`, states each verb's consequence, and for a deploy names the changed projections, rebuilds, out-of-band overwrites, faulted targets, and any `[database_config]` divergence. Recreate and delete destroy state with no undo, so they ask every time; on production their confirmation requires typing the projection name, and a production deploy plan containing `resetOnLogicChange` rebuilds requires typing the environment name. A client without elicitation support cannot perform gated writes; the refusal names the CLI command to run instead.
