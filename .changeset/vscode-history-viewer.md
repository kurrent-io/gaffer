---
"gaffer-vscode": patch
---

The per-projection **Manage...** menu gains a **History** action: a timeline of the projection's deploys on an environment, in an editor tab. Each version shows its operation, content hash, actor, and time, drawn as a graph that reads run state (enabled/disabled/deleted), reverts, and recreates. It's the same grammar as `gaffer history` in the terminal.

- Hovering a content version reveals its actions: diff it against the previous version or against your local source (both open VS Code's diff editor), or roll back to it.
- Rollback rewrites the live query to the chosen version behind the native confirm (silent off-production, a modal accept on production). State is kept and local files are left untouched, so they show as drift until updated.
