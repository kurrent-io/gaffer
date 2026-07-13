---
"gaffer-vscode": patch
---

The extension now shows read-only deployment status above each `[env.<name>]` block in `gaffer.toml`. Each environment gets a roll-up of how its projections compare to what's deployed, a **Sign in** action when it needs authentication, or a **status unavailable** note when the read can't complete. A production target is flagged **PRODUCTION**. Status is read on open and save.
