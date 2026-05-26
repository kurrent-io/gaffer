---
"gaffer-vscode": patch
---

Strip Unix extra fields from the appended `node_modules/gaffer-tsserver-plugin` zip entries so Open VSX accepts the `.vsix`. 0.1.0's first publish was rejected with "extension file contains zip entries with potentially harmful extra fields".
