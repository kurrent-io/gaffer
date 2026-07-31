---
"gaffer-vscode": patch
---

The per-projection **Manage...** menu gains a **History** action: a timeline of the projection's deploys on an environment, in an editor tab. Each version shows its operation, content hash, actor, and time, drawn as a graph that reads run state (enabled/disabled/deleted), reverts, and recreates. It's the same grammar as `gaffer history` in the terminal.

**Two panes.** The timeline sits on the left; selecting a version opens a detail panel on the right with its metadata (when, run state, content hash, actor, tool, operation, and source) and its actions. Timeline rows are keyboard-navigable, and the panel drops below the timeline on a narrow editor. The panel takes the width its content needs from the timeline, up to a cap, keeping its action buttons on one line where there's room.

**A version's actions** sit in that panel: diff it against the previous version or against your local source (both open VS Code's diff editor), or roll back to it. Rollback rewrites the live query to the chosen version behind the native confirm (silent off-production, a modal accept on production). State is kept and local files are left untouched, so they show as drift until updated. The timeline refreshes as soon as a rollback lands, rather than waiting on the success toast being dismissed, and history and rollback get a spawn timeout long enough for a slow or remote cluster - connecting, resolving a version, and reading or writing didn't fit the 10s default.

**Attribution follows the CLI.** A version is flagged as changed outside gaffer from the `outOfBand` field rather than from the kind, and only once gaffer has been managing the projection, so writes on a server that doesn't preserve gaffer's metadata no longer all show as external edits. A metadata-less update shows what it changed, e.g. `query changed`, matching the terminal; an out-of-band edit reads `query changed outside gaffer` rather than the less specific `changed outside gaffer`.
