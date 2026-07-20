---
"gaffer-vscode": patch
---

The per-projection **Manage...** menu now offers the operate verbs alongside the diff: **Pause**, **Resume**, **Abort**, and **Delete** (with a **Delete (and emitted streams)** variant). The menu shows Pause or Resume based on the projection's current runtime state. Each verb confirms before running: a non-production reversible verb runs straight away, a production or irreversible verb asks you to confirm, and deleting on production asks you to type the projection's name. It then runs over the language server's warm connection and reports the result.
