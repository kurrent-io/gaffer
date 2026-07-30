---
title: Environments and targets
description: Model your KurrentDB targets as environments in gaffer.toml, select them with --env, and keep each one's credentials in .env files.
---

An environment is a named KurrentDB target in `gaffer.toml`. Every command that touches a server (`deploy`, `status`, `diff`, and the rest) resolves one, so growing a project past the single local database from [Deploy your first projection](./deploy.mdx) means adding blocks, not changing commands.

## One block per target

A project that develops locally, verifies on staging, and ships to production declares all three:

```toml
[env.local]
connection = "kurrentdb://localhost:2113?tls=false"
default = true

[env.staging]
connection = "kurrentdb://staging.internal:2113"

[env.prod]
connection = "kurrentdb+discover://db.example.com:2113"
production = true
```

Each `[env.<name>]` block is self-contained: there is no inheritance or shared base, so every block declares its own full connection and authentication. `default = true` marks the environment used when `--env` is omitted, and at most one block may carry it. `production = true` opts an environment into the [production guard tier](./production.md), where confirmations name the target as production and some bypasses are refused. See [`[env.<name>]`](../reference/gaffer-toml.md#envname) for every field.

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

![gaffer deploy --env staging planning against the staging target and asking to confirm](/demo-deploy-staging.gif)

Deploy converges each environment separately: the projection created on `local` in the previous step doesn't exist on `staging` until you deploy it there, and each server's plan is computed against what that server holds.

Without `--env`, the default environment is used. With no default, only `gaffer dev` falls back to an interactive picker (see [Interactive mode](../cli/index.md#interactive-mode)); the other live commands error, asking for `--env`, `--connection`, or a default.

`--connection` takes a raw connection string for a one-off target without editing `gaffer.toml`. It's an override, not an environment: none of a block's settings apply, including authentication and the `production` opt-in.

:::note
Environments are for live servers. Fixture runs (`gaffer dev --fixture` / `--events`) need no environment, and the two source kinds are mutually exclusive on the command line.
:::

## Authenticate per environment

Credentials live next to `gaffer.toml` in `.env` files, not in it. A per-environment `.env.<env>` file (matching the selected environment) overlays the base `.env` at the project root, and values resolve highest precedence first: the shell environment, then `.env.<env>`, then `.env`. Each target carries its own credentials, and a value set in your shell or injected by CI is never overwritten by either file. Keep `.env` files out of version control; see [Environment file](../cli/index.md#environment-file-env).

### Basic authentication

Basic authentication uses a username and password, and is the default scheme (and the only one on Kurrent Cloud managed clusters). gaffer reads the standard variables, so `.env.staging` is all the setup:

```sh title=".env.staging"
KURRENTDB_USERNAME=admin
KURRENTDB_PASSWORD=changeit
```

With that file in place, `gaffer deploy --env staging` connects as `admin` while the committed `[env.staging]` block stays a bare connection string. See [Basic authentication](../cli/authentication.md#basic-authentication).

### Client certificate

An environment can authenticate with an X.509 user certificate instead of a password. Set both files on the block:

```toml
[env.prod]
connection     = "kurrentdb+discover://db.example.com:2113"
production     = true
user_cert_file = "certs/prod-user.crt"
user_key_file  = "certs/prod-user.key"
```

Both must be set together, and the certificate is presented in the TLS handshake, so the connection must use TLS (the default). See [Client certificate](../cli/authentication.md#client-certificate).

### OAuth / OIDC

An environment can instead authenticate through an identity provider with bearer tokens. Add an `[env.<name>.oauth]` block:

```toml
[env.prod]
connection = "kurrentdb+discover://db.example.com:2113"
production = true

[env.prod.oauth]
issuer    = "https://idp.example.com/realms/kurrent"
client_id = "kurrentdb-client"
```

Run `gaffer auth --env prod` once to sign in through the browser; the token is stored and refreshed automatically. CI sets `KURRENTDB_OAUTH_CLIENT_SECRET` (through the same `.env` layering or the pipeline's own secrets) to use the non-interactive grant instead. See [OAuth / OIDC](../cli/authentication.md#oauth--oidc).

## Replacement variables

Beyond the standard variables, any part of a connection string (and the certificate paths) can reference an environment variable with `${VAR}`, for values that differ per machine or can't appear in the file at all:

```toml
[env.staging]
connection = "kurrentdb://admin:${DB_PASSWORD}@staging.internal:2113"

[env.prod]
connection = "${KURRENT_PROD_CONNECTION}"
production = true
```

References resolve with the same precedence as the `.env` layering above, and a referenced variable that isn't set is an error, so a missing value fails fast instead of connecting without it. Only the braced `${...}` form is a reference; a bare `$` is left untouched. See [`[env.<name>]`](../reference/gaffer-toml.md#envname).

## Next steps

The `production` flag is one half of the guard model; [Production safety guards](./production.md) covers the other half and what the tier changes. The [Authentication](../cli/authentication.md) page covers each scheme's full setup, including token storage and keyrings.
