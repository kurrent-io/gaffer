---
"gaffer-vscode": patch
---

Scaffold from the command palette now skips the partitioning step for a single-stream source, where per-stream partitioning isn't valid, matching the CLI.
