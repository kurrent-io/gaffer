---
"gaffer-vscode": patch
---

The deploy-plan panel no longer wedges after you dismiss the deploy confirmation. Previously, clicking Deploy and cancelling the native confirm modal - or deploying from an untrusted workspace - left the panel ignoring every later preview and deploy until you closed and reopened it. The panel now recovers on those paths, and also when a deploy fails to start, re-enabling the Deploy button.
