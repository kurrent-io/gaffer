---
"@kurrent/gaffer": patch
---

`gaffer history <projection>` shows a deployed projection's history: every operation on it, newest first, with who made it and how.

- On a terminal it opens an interactive timeline: a scrolling list on the left, the selected entry's full detail on the right, and a footer naming the projection and target. Navigate with `↑`/`↓` (or `j`/`k`), `g`/`G`, `PgUp`/`PgDn`; `q` or `Esc` quits. Older entries page in as you scroll.
- Each entry is one write to the projection. One carrying gaffer metadata shows its operation (deploy, rollback, reset), the actor, and the source revision. One without is attributed by what changed: `edited-externally` when the definition changed outside gaffer, `changed-by` (with the tool named) for another tool's write, `enabled`/`disabled` for a lifecycle change, `reconfigured` when a checkpoint or performance setting moved, `rewritten` for an identical redeploy, or `created`/`deleted`.
- A content hash identifies each deployed definition, so a reverted definition is recognisable at a glance: the timeline draws a revert as a branch off the live line, linking the restored definition back to the earlier one it matched (nested reverts included).
- Piped or with `--json` it prints the latest entries instead (`--limit`, default 100, or `--all`). Each `--json` entry carries the full content hash, its classification and flags, the tool metadata, and any configuration knobs that moved.

Against a KurrentDB without the deploy-metadata field it degrades to the history with timestamps and content hashes only.
