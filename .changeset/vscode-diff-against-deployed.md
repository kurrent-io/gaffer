---
"gaffer-vscode": patch
---

Each `[[projection]]` header in `gaffer.toml` now carries an **actions..** lens that opens a per-projection action menu, grouped by environment. Its first action, **Diff against deployed**, opens VS Code's native diff editor comparing the projection's local source against what's deployed on the chosen environment. Both sides are read-only. A projection that isn't deployed shows a message instead of an empty diff, and an environment that needs authentication offers a one-click sign-in.
