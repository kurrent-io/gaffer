---
"gaffer-vscode": patch
---

The extension isolates its encrypted-file token store at `keyring-vscode` (via `GAFFER_KEYRING_NAME`) on a host with no OS keyring, keeping it separate from the CLI's default store. The random passphrase the extension injects to unlock its own store therefore never locks the store a manual terminal `gaffer` uses.
