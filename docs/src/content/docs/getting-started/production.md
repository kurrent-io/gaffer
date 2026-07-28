---
title: Production safety guards
description: How gaffer decides a database is production, and what deploy and the operate verbs confirm or refuse against one.
---

Deploying and operating projections can destroy state, so gaffer guards every write behind a confirmation, and holds a higher tier of guard for production databases.

## How a target becomes production

Two signals raise the tier, and either one is enough:

- **The database declares itself.** A server reports its identity in the `$server-info` system stream (one `$ServerInfo` event per update, latest wins), with an optional `production` flag. Set it from [Navigator](https://navigator.kurrent.io) or the database's embedded web UI (Admin > Database Info). Because the declaration lives in the database, it travels with the cluster no matter who connects or what their local config calls the environment.
- **The environment opts in.** [`production = true`](../reference/gaffer-toml.md#envname) on the `[env.<name>]` block marks everything reached through that environment as production.

The signals combine as an OR, so config can add the tier but never remove it. Only an explicit `production = true` in `$server-info` earns the tier from the server side; `false`, unset, and an absent stream are all baseline. Setting `production = false` on an environment is the same as omitting it: against a server that declares itself production, it is a silent no-op, so a local `gaffer.toml` can never disarm a production database.

## What the tier changes

Against a baseline target, `gaffer deploy` shows its plan and asks a plain confirmation. Against a production target:

- **Confirmations get louder.** The plan carries a red `PRODUCTION` banner, and the prompt names the target as production (`Apply 2 changes to production orders?`). This applies to deploy and every guarded operate verb: `delete` and `recreate` always confirm, `rollback` confirms with a diff, and `disable` confirms only against production.
- **`--no-validate` is refused.** Skipping validation deploys projections that were never compiled, which production never permits. `gaffer deploy --no-validate` and `gaffer recreate --no-validate` against a production target exit `3` (refused by a guardrail) without writing anything. See [exit codes](../cli/index.md#exit-codes).

`--yes` still works: it is an explicit confirmation, so a script that passes it deploys to production without a prompt. The tier makes intent explicit rather than making production untouchable; the blanket bypasses it refuses are the silent ones.

## The invisible tier

The `$server-info` read is best-effort. When the stream is unreadable (ACL-restricted on a secured server, or a database too old to have it) and the environment doesn't opt in, a real production cluster silently gets baseline guards: plain confirmations, `--no-validate` permitted.

:::caution
Don't rely on the server declaring itself. Set `production = true` on every environment that points at a production database, so the tier holds even when the declaration can't be read - and holds for teammates whose credentials can't read the stream.
:::

## One model across surfaces

The target's name and tier are resolved in one place, so a server never rates a different tier depending on which tool reaches it. The [VS Code extension](../extension/vs-code.md) actions and the [MCP server](../cli/mcp.md)'s deploy and operate tools share the CLI's resolution, and both add a stronger confirmation on top: a no-undo write against production asks you to type the environment or projection name, where the CLI prompt is a plain confirm.

## Next steps

Environments and their `production` opt-in are covered in [Environments and targets](./environments.md); the full field reference is [`[env.<name>]`](../reference/gaffer-toml.md#envname).
