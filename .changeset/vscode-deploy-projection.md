---
"gaffer-vscode": patch
---

The per-projection **Manage...** menu gains a **Deploy** action alongside diff and the operate verbs. It opens the deploy plan scoped to just that projection against the chosen environment, in the same editor-tab webview as the whole-project Deploy lens, and applies it behind the same native confirm. The single-projection plan resolves its own change and bundles a recreate when one's needed.
