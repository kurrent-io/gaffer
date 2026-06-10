---
"gaffer-vscode": patch
---

The command palette **Gaffer: Debug** now offers configured environments, matching the CodeLens picker. After you choose a projection it lists every fixture and every `[env.<name>]`, with the default tagged, so a non-default environment is reachable from the palette. Previously it only knew the default connection, leaving multi-environment projects unable to pick another env there.
