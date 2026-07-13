---
"gaffer-vscode": patch
---

Show read-only deployment status above each `[env.<name>]` block in `gaffer.toml`. Each environment gets a roll-up of how its projections compare to what's deployed, a **Sign in** action when it needs authentication, or a **status unavailable** note when the read can't complete. A production target carries a **PROD** badge. Status is read on open and save, and **Gaffer: Refresh Deployment Status** re-reads it on demand.
