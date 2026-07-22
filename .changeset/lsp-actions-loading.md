---
"@kurrent/gaffer": patch
---

The language server's per-projection actions lens payload gains a `loading` flag per environment, set while that environment's status fetch is still in flight. The VS Code "Manage..." menu uses it to show a spinner and settle into the resolved actions in place.
