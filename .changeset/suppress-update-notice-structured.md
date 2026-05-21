---
"@kurrent/gaffer": patch
---

`gaffer`'s update-check pipeline now separates the stderr notice from the registry refresh.

- The "Update available" stderr notice is suppressed on machine-readable invocations: `gaffer manifest`, `gaffer lsp`, `gaffer mcp`, or any command run with `--json`. Previously the notice could print onto the sibling stream of a structured stdout payload when stderr was a TTY (e.g. `gaffer manifest | jq`).
- The once-per-day registry refresh now runs on non-interactive paths too. Previously it was gated on the same TTY check as the notice, so a user invoking gaffer only through an editor wrapper would have a stale-forever cache. The refresh is still skipped under `--no-update-check` and `GAFFER_NO_UPDATE_CHECK=1`.
