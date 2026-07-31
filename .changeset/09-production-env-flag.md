---
"@kurrent/gaffer": patch
---

A `production = true` flag on an `[env.<name>]` block marks the environment's database as production, activating the production guard tier locally. Deploy and operate confirmations name the target as production, and `--no-validate` is refused.

- The flag combines with the database's own `$server-info` declaration as an OR, so it is opt-in only. `production = false` (the same as omitting it) defers to the server, and config can never downgrade a database that declares itself production. This activates the guardrail for the production databases that don't populate `$server-info` yet.
- Confirmation prompts and messages now name the resolved environment when the server doesn't report a cluster name, including runs on the default environment, which previously showed no target name.
- The history timeline gates like `gaffer rollback` does: its footer carries a production badge, and the rollback confirm names the production target.
