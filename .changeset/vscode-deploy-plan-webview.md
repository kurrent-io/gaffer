---
"gaffer-vscode": patch
---

Each `[env.X]` block in `gaffer.toml` gains a **Preview** lens beside its status roll-up. Clicking it runs `gaffer deploy --dry-run --json` for the whole project against that env and renders the plan in an editor-area webview.

- The plan lists every projection with its would-be change (create, update, rebuild, recreate, unchanged, or invalid) and the warnings that matter before applying: faulted, re-emits, a logic change, and a definition changed outside gaffer. It leads with the resolved target and a production pill, ends with a per-action summary, and surfaces any `[database_config]` divergence.
- An updated projection has a **Diff** action that opens its native deployed-vs-local diff; an invalid one shows its compile error inline.
- Read-only: Preview stops at the plan and never applies. It runs as a cold spawn so the preview resolves the way a real deploy will; a token that has expired offers a sign-in.
