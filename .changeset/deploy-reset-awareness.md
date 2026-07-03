---
"@kurrent/gaffer": patch
---

`gaffer deploy` now treats a changed projection query as a logic change. The new code may read already-processed events differently, so the accumulated state could be wrong. By default deploy keeps the checkpoint, applies the update, and flags the change in the plan. Pass `--reset-on-logic-change` to rebuild instead: each logic-changed projection is stopped, updated, reset to the beginning, and restarted so it reprocesses from zero with the new logic. An emitting projection re-emits on a rebuild and may duplicate into its target streams, so the plan warns and points at `gaffer recreate --delete-emitted` for a clean-emit rebuild.

A continued logic change shows as `logicChange: true` on its `--json` item, so CI can alert on it. A change to engine version or track-emitted-streams still can't be applied in place; deploy now points at `gaffer recreate` rather than just refusing.
