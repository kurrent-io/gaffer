---
"gaffer-vscode": patch
---

Each `[env.X]` block in `gaffer.toml` gains a **Deploy** lens leading the block, ahead of its status roll-up, and the per-projection **Manage...** menu's **Deploy** action does the same for a single projection. Both open the deploy plan in an editor-area webview, to review and then deploy. The lens is scoped to the whole project against that env; the menu action is scoped to just that projection, resolving its own change and bundling a recreate when one's needed.

- The plan lists every projection with its change (create, update, rebuild, recreate, unchanged, or invalid) and the warnings that matter: faulted, re-emits, a logic change, and a definition changed outside gaffer. It leads with the resolved target and a production pill, and surfaces any `[database_config]` divergence. An updated projection offers a **Diff** against what's deployed; an invalid one shows its compile error inline, wrapped in place rather than scrolling sideways.
- **Deploy** applies the plan behind a native confirm. The tier follows the target: silent off production with no rebuild, a modal accept when production or a rebuild is involved, and typing the environment name for a production rebuild. Dismissing the confirm, deploying from an untrusted workspace, or a deploy that fails to start all leave the panel usable, with the Deploy button re-enabled.
- A blocked plan can't deploy; off production, a checkbox deploys the valid projections and skips the rest (`--no-validate`). The apply streams each projection's row in place, then a result summary. It's a cold spawn on the same auth path as the preview, so what you review is what deploys.

The untrusted-workspace notice names deploy and operate among the CLI-driven actions disabled until the workspace is trusted.
