---
"@kurrent/gaffer": patch
"gaffer-vscode": patch
---

Diagnostics now link to a reference page at [gaffer.kurrent.io/reference/diagnostics](https://gaffer.kurrent.io/reference/diagnostics/), generated from the diagnostic catalog with one entry per `quirk.*` / `usage.*` code. The `gaffer dev` and test summaries print a `See <url>` line after the quirk list, and the VS Code step-warning panel makes each quirk a clickable link to its entry.
