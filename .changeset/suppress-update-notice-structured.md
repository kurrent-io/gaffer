---
"@kurrent/gaffer": patch
---

Suppress the "Update available" stderr notice when the invocation emits machine-readable output (`gaffer manifest`, `gaffer lsp`, `gaffer mcp`, or any command run with `--json`). Previously the notice could print onto the sibling stream of a structured stdout payload when stderr happened to be a TTY (e.g. `gaffer manifest | jq` in a terminal), forcing wrappers and pipes to filter human-readable noise.
