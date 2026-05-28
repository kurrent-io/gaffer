---
"gaffer-vscode": patch
---

The `gaffer not installed` prompt no longer persists on Windows after `npm install -g @kurrent/gaffer`. CLI spawn sites now route through `cross-spawn`, which honours `PATHEXT` and resolves the `gaffer.cmd` shim that npm drops into `%APPDATA%\npm`.
