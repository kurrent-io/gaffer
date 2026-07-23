---
"gaffer-vscode": patch
---

The history viewer follows the CLI's revised attribution. It flags a version as changed outside gaffer from the `outOfBand` field rather than from the kind, and only after gaffer has been managing the projection. So writes on a server that doesn't preserve gaffer's metadata no longer all show as external edits.
