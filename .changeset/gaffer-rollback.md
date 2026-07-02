---
"@kurrent/gaffer": patch
---

`gaffer rollback <projection> <hash>` rolls a deployed projection back to a prior version from its history, stamped `operation: rollback` in the deploy ledger. The target is named by its content hash from `gaffer history`; any unique prefix of 4 or more characters works. It confirms first with the current-to-target query diff (`--yes` skips). The apply is in place: processing continues from the current checkpoint, and local files stay untouched, so `gaffer diff` shows the rollback as drift until local is reconciled. A version differing in engine version or emitted-stream tracking is refused, pointing at `gaffer recreate`.

The `gaffer history` timeline gains `r`: it opens the same confirm as a modal for the selected entry, applies on `y`, and reloads the timeline so the new rollback entry appears on top.
