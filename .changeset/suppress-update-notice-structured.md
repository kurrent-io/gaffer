---
"@kurrent/gaffer": patch
---

Split the update-check pipeline so machine-readable invocations stay quiet without going dark:

- The "Update available" stderr notice is now suppressed whenever the invocation emits machine-readable output (`gaffer manifest`, `gaffer lsp`, `gaffer mcp`, or any command run with `--json`). Previously the notice could print onto the sibling stream of a structured stdout payload when stderr was a TTY (e.g. `gaffer manifest | jq`).
- The once-per-day registry refresh now runs on non-interactive paths too. Previously the refresh was gated on the same TTY check that gated the notice, so a user who only ever invoked gaffer through an editor wrapper would have a stale-forever cache and the wrapper's `updateAvailable` signal would never fire. The refresh is still skipped under `--no-update-check` and `GAFFER_NO_UPDATE_CHECK=1`.
