---
"gaffer-vscode": patch
---

Each `[[projection]]` header in `gaffer.toml` now carries a **Manage...** lens that opens a per-projection action menu, grouped by environment: **History**, **Diff against deployed**, **Deploy**, and the operate verbs **Pause**, **Resume**, **Abort**, and **Delete**.

- **Diff against deployed** opens VS Code's native diff editor comparing the projection's local source against what's deployed on the chosen environment. Both sides are read-only. A projection that isn't deployed shows a message instead of an empty diff.
- **The operate verbs** run over the language server's warm connection and report the result. The menu shows Pause or Resume based on the projection's current runtime state, and deleting a projection that emits streams asks whether to remove those too. Each verb confirms before running: a non-production reversible verb runs straight away, a production or irreversible verb asks you to confirm, and deleting on production asks you to type the projection's name.

**Each environment shows its own state, and collapses when it has nothing to offer.** One that needs authentication collapses to a single **Sign in**; one whose status couldn't be read collapses to a single **Unavailable**, rather than listing actions that can't run against an unreachable environment. The menu opens immediately and shows a **Loading status…** row for any environment still being read, filling in that environment's actions in place as the status resolves, so a still-loading environment is never a dead-end that needs closing and reopening.
