---
"@kurrent/gaffer": patch
---

`gaffer recreate` now stamps its rebuild in the deploy ledger, so `gaffer history` attributes it to gaffer instead of showing anonymous lifecycle steps. The create carries `operation: recreate` with the usual tool, actor, and source revision.

`gaffer history` shows a recreate as a single entry: the disable and delete writes it performs are folded into the `recreate` row, and the detail panel notes the projection was reprocessed from zero. `--json` keeps every write as its own entry, with the create's `kind` set to `recreate`.
