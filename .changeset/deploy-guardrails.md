---
"@kurrent/gaffer": patch
---

`gaffer deploy` now plans the whole run before touching the server and confirms before applying. It shows what would change per projection (`created` / `updated` / `skipped` / `refused`) against the target's reported cluster name, then asks before writing. `--yes` skips the prompt; without a terminal (or with `--json`) it won't apply unconfirmed, so pass `--yes` in scripts. An update whose deployed projection is currently faulted is flagged, since updating won't clear the fault.

A server that reports itself as production gets a louder confirmation and refuses `--no-validate`. Production is read from the server's own `$server-info`, never inferred from the environment name, so a connection that points at production is guarded even if its env is labelled otherwise. Databases that don't report production status are unaffected.
