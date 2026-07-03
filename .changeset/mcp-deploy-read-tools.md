---
"@kurrent/gaffer": patch
---

The MCP server gains read-only deploy visibility: `deploy_status` shows each projection's runtime state and drift verdict on an environment (mirroring `gaffer status --json`, including `configDrift`), `deploy_plan` previews what a deploy would change without applying anything (mirroring `gaffer deploy --dry-run --json`), and `deploy_history` reads a projection's per-deploy audit log with paging (mirroring `gaffer history --json`). All three accept an `env` argument and default to the default environment. The deploy action itself stays in the CLI.
