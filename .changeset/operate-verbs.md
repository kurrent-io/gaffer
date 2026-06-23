---
"@kurrent/gaffer": patch
---

`gaffer start`, `gaffer stop`, and `gaffer delete` manage deployed projections on an environment, named directly (they need not be in `gaffer.toml`).

- `gaffer start <projection>` enables a projection so it resumes from its last checkpoint.
- `gaffer stop <projection>` disables it, writing a final checkpoint; `--abort` skips that checkpoint so a later start replays from the last one. Stopping is recoverable, so it confirms only against production.
- `gaffer delete <projection>` removes the projection with its state and checkpoint streams, keeping emitted streams unless `--delete-emitted` is passed. It always confirms, and disables the projection first since the server won't delete an enabled one.

`--yes` skips the confirmation; without a terminal (or with `--json`) a guarded verb won't proceed unconfirmed. Production gets a louder confirm and is read from the server's `$server-info`, never the env label.
