---
"gaffer-vscode": patch
---

Each `[env.X]` block in `gaffer.toml` gains a **Preview** lens beside its status roll-up. Clicking it runs `gaffer deploy --dry-run --json` for the whole project against that env and renders the plan in an editor-area webview.

- The plan lists every projection with its would-be change (create / update / rebuild / skipped / refused / invalid) and the warnings that matter before applying: recreate-required, faulted, re-emits, logic change, and a definition changed outside gaffer. It leads with the env, resolved target, a production badge, and the overall verdict, and surfaces any `[database_config]` divergence.
- Clicking a projection opens its native deployed-vs-local diff.
- Read-only: Preview stops at the plan and never applies. It runs as a cold spawn so the preview resolves the way a real deploy will; a token that has expired offers a sign-in.
