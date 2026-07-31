---
"@kurrent/gaffer": patch
---

`gaffer enable`, `gaffer disable`, and `gaffer delete` manage deployed projections on an environment, named directly (they need not be in `gaffer.toml`).

- `gaffer enable <projection>` starts a projection so it resumes from its last checkpoint.
- `gaffer disable <projection>` stops it, writing a final checkpoint; `--abort` skips that checkpoint so a later enable replays from the last one. Disabling is recoverable, so it confirms only against production.
- `gaffer delete <projection>` removes the projection with its state and checkpoint streams, keeping emitted streams unless `--delete-emitted` is passed. It always confirms, and disables the projection first since the server won't delete an enabled one.

`--yes` skips the confirmation; without a terminal (or with `--json`) a guarded verb won't proceed unconfirmed. Production gets a louder confirm and is read from the server's `$server-info`, never the env label.
