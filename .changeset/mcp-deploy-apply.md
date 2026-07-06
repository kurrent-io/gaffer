---
"@kurrent/gaffer": patch
---

The MCP server gains **`deploy_apply`**, so an assistant can deploy projections from `gaffer.toml` like `gaffer deploy`:

- The same all-or-nothing compile and diagnostics preflight, with no validation bypass.
- The same per-item results as `gaffer deploy --json`, and every write stamped `operation: deploy` in the ledger.
- A production deploy requires a confirmation answered through the MCP client (elicitation). The prompt names the changed projections, rebuilds, out-of-band overwrites, faulted targets, and any `[database_config]` divergence.
- A plan containing `resetOnLogicChange` rebuilds destroys state, so it always asks; on production that confirmation requires typing the environment name.
