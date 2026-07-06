---
"@kurrent/gaffer": patch
---

The MCP server gains read-only deploy visibility, mirroring the CLI's machine output:

- **`deploy_status`** shows each projection's runtime state and drift verdict on an environment, plus any `[database_config]` divergence, like `gaffer status --json`.
- **`deploy_plan`** previews what a deploy would change without applying anything, like `gaffer deploy --dry-run --json`.
- **`deploy_history`** reads a projection's per-deploy audit log with paging, like `gaffer history --json`.

All three accept an `env` argument and default to the default environment. Applying changes stays in the CLI, behind its confirmation gates.
