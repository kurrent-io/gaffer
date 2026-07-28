---
title: Environments and targets
description: Model your KurrentDB targets as environments in gaffer.toml, select them with --env, and keep each one's credentials in .env files.
---

An environment is a named KurrentDB target in `gaffer.toml`. Every command that touches a server (`deploy`, `status`, `diff`, and the rest) resolves one, so growing a project past the single local database from [Deploy your first projection](./deploy.md) means adding blocks, not changing commands.

## One block per target

A project that develops locally, verifies on staging, and ships to production declares all three:

```toml
[env.local]
connection = "kurrentdb://localhost:2113?tls=false"
default = true

[env.staging]
connection = "kurrentdb://admin:${DB_PASSWORD}@staging.internal:2113"

[env.prod]
connection = "${KURRENT_PROD_CONNECTION}"
production = true
```

Each `[env.<name>]` block is self-contained: there is no inheritance or shared base, so every block declares its own full connection and authentication. `default = true` marks the environment used when `--env` is omitted, and exactly one block may carry it. `production = true` opts an environment into the production guard tier, where confirmations name the target as production and some bypasses are refused. See [`[env.<name>]`](../reference/gaffer-toml.md#envname) for every field.

## Select a target

Pass `--env` to any live command:

```sh
gaffer deploy --env staging
```

The plan names the target, so what you're about to change is always visible:

```
Plan for staging:
  order-count  create
  1 to create
```

<!-- TODO(media): vhs tape - gaffer deploy --env staging: plan heading naming the target, confirm prompt -->

Deploy converges each environment separately: the projection created on `local` in the previous step doesn't exist on `staging` until you deploy it there, and each server's plan is computed against what that server holds.

Without `--env`, the default environment is used. With no default, only `gaffer dev` falls back to an interactive picker (see [Interactive mode](../cli/index.md#interactive-mode)); the other live commands error, asking for `--env`, `--connection`, or a default.

`--connection` takes a raw connection string for a one-off target without editing `gaffer.toml`. It's an override, not an environment: none of a block's settings apply, including authentication and the `production` opt-in.

:::note
Environments are for live servers. Fixture runs (`gaffer dev --fixture` / `--events`) need no environment, and the two source kinds are mutually exclusive on the command line.
:::

## Keep secrets out of the file

A connection string can reference variables with `${VAR}`, so `gaffer.toml` commits cleanly while each credential stays local. Values resolve, highest precedence first, from the shell environment, then a per-environment `.env.<env>` file, then the base `.env` at the project root:

```sh title=".env.staging"
DB_PASSWORD=changeit
```

Selecting `--env staging` overlays `.env.staging` on `.env`, so each environment carries its own credentials without touching the others, and a variable set in your shell or injected by CI is never overwritten by either file. A referenced variable that isn't set is an error, so a missing secret fails fast instead of connecting without it. Keep `.env` files out of version control; see [Environment file](../cli/index.md#environment-file-env).

## Authenticate per environment

Authentication is declared per block, so each target can use a different scheme:

- **Basic**: supply `KURRENTDB_USERNAME` / `KURRENTDB_PASSWORD` through the environment (or `user:password@` in the connection string). See [Basic authentication](../cli/authentication.md#basic-authentication).
- **Client certificate**: set `user_cert_file` / `user_key_file` on the block. See [Client certificate](../cli/authentication.md#client-certificate).
- **OAuth / OIDC**: add an `[env.<name>.oauth]` block, then `gaffer auth --env <name>` signs in through the browser; CI sets `KURRENTDB_OAUTH_CLIENT_SECRET` for the non-interactive grant. See [OAuth / OIDC](../cli/authentication.md#oauth--oidc).

The production block from above, authenticated with a client certificate:

```toml
[env.prod]
connection     = "${KURRENT_PROD_CONNECTION}"
production     = true
user_cert_file = "certs/prod-user.crt"
user_key_file  = "${PROD_CERT_DIR}/prod-user.key"
```

## Next steps

The `production` flag is one half of the guard model; a database can also declare itself production, and the two combine so config can never disarm a production server. The [Authentication](../cli/authentication.md) page covers each scheme's full setup, including token storage and keyrings.
