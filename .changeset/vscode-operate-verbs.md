---
"gaffer-vscode": patch
---

The per-projection **Manage...** menu now offers the operate verbs alongside the diff: **Pause**, **Resume**, **Abort**, and **Delete**. Deleting a projection that emits streams asks whether to remove those too. The menu shows Pause or Resume based on the projection's current runtime state. Each environment shows its status, and one that needs authentication offers Sign in instead of the actions. Each verb confirms before running: a non-production reversible verb runs straight away, a production or irreversible verb asks you to confirm, and deleting on production asks you to type the projection's name. It then runs over the language server's warm connection and reports the result.
