---
"gaffer-vscode": patch
---

Clicking **Debug** on Windows no longer fails with a misleading "Timeout waiting for debug message". The IPC debug spawn now routes through `cross-spawn`, which resolves the npm-installed `gaffer.cmd` shim, and a spawn that never starts surfaces immediately as an exit instead of waiting out the full timeout.
