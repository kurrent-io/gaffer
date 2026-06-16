---
title: gaffer.toml
description: Full reference for the gaffer.toml project configuration file.
---

`gaffer.toml` lives at the root of a gaffer project and declares its projections, the environments they connect to, and per-projection settings. `gaffer init` writes the initial file.

:::caution[Upgrading from an older gaffer.toml]
Top-level `connection` and top-level `engine_version` are no longer supported. Loading a file with either key fails with a migration hint.

- A top-level `connection` moves into an [`[env.<name>]`](#envname) block.
- A top-level `engine_version` moves onto each [`[[projection]]`](#engine_version) (every projection now sets its own).

Before:

```toml
connection = "kurrentdb://localhost:2113?tls=false"
engine_version = 2

[[projection]]
name = "order-count"
entry = "projections/order-count.js"
```

After:

```toml
[env.local]
connection = "kurrentdb://localhost:2113?tls=false"
default = true

[[projection]]
name = "order-count"
entry = "projections/order-count.js"
engine_version = 2
```
:::

## Example

```toml
telemetry = true

[env.local]
connection = "kurrentdb://localhost:2113?tls=false"
default = true

[[projection]]
name = "order-count"
entry = "projections/order-count.js"
engine_version = 2
fixtures.happy = "fixtures/orders.json"
fixtures.full = "fixtures/orders-full.json"

[[projection]]
name = "order-totals"
entry = "projections/order-totals.js"
engine_version = 2
```

## Top-level keys

### `[env.<name>]`

```toml
[env.local]
connection = "kurrentdb://localhost:2113?tls=false"
default = true

[env.staging]
connection = "kurrentdb://admin:${DB_PASSWORD}@staging:2113"
```

An environment names a KurrentDB connection. Select one with `gaffer dev --env <name>`, or pick it from the interactive prompt. Environment names must match `^[A-Za-z0-9_-]+$`.

- **`connection`**: KurrentDB connection string for the environment. Required. Used when running a projection against a live event stream (`gaffer dev <projection>` without `--events` or `--fixture`). Override per-invocation with `--connection`.
- **`default`**: optional bool. Exactly one environment may set `default = true`. It's used when `--env` is omitted; without a default, an interactive `gaffer dev` prompts you to pick an environment, while a non-interactive run requires `--env` (fixture runs need no environment). Two defaults is a config error.
- **`user_cert_file`** / **`user_key_file`**: optional paths to an X.509 user certificate and its private key, for authenticating to KurrentDB with a client certificate. Both must be set together. The certificate is presented in the TLS handshake, so the connection must use TLS. A client certificate is independent of OAuth, so an environment may use both. Like `connection`, the paths expand `${VAR}` references and resolve relative to the project root when not absolute, so a relative path works regardless of the directory `gaffer` runs from. It is an enterprise or self-hosted feature, not available on Kurrent Cloud.

```toml
[env.staging]
connection     = "kurrentdb://staging:2113?tls=true"
user_cert_file = "certs/user.crt"
user_key_file  = "${CERT_DIR}/user.key"
```

`${VAR}` references in `connection` are expanded so credentials need not be committed:

```toml
[env.staging]
connection = "kurrentdb://admin:${DB_PASSWORD}@staging:2113"
```

Values resolve, highest precedence first, from the shell environment, then a per-environment [`.env.<env>`](../cli/index.md#environment-file-env) file, then the base [`.env`](../cli/index.md#environment-file-env) file at the project root. A referenced variable that isn't set is an error; only the braced `${...}` form is a reference, so a bare `$` is left untouched.

### `[env.<name>.oauth]`

```toml
[env.staging.oauth]
issuer    = "https://idp.example.com/realms/kurrent"
client_id = "kurrentdb-client"
scopes    = ["openid"]
```

Authenticate to the environment with OAuth/OIDC bearer tokens instead of a username and password. The KurrentDB cluster must be configured for OAuth (a licensed feature, not available on Kurrent Cloud). Endpoints are discovered from the issuer's `/.well-known/openid-configuration`.

- **`issuer`**: OIDC issuer URL. Required. Must be `https` (an `http` loopback issuer is allowed for local development).
- **`client_id`**: OAuth client ID. Required.
- **`scopes`**: optional list of scopes to request.
- **`audience`**: optional audience parameter, for identity providers that require one (e.g. Auth0).
- **`ca_file`**: optional path (relative to the project root, or absolute) to a PEM CA bundle for verifying the issuer's TLS, when the provider is served by an internal or self-signed CA. A CA certificate is public, so it lives here rather than in the environment.

The **client secret is never written to `gaffer.toml`**. Its presence in the environment selects how a token is obtained:

- **With `KURRENTDB_OAUTH_CLIENT_SECRET` set**: gaffer uses the non-interactive client-credentials grant, for CI and automation. The secret resolves with the same precedence as `connection` variables (shell, then `.env.<env>`, then `.env`).
- **Without it**: run `gaffer auth --env <name>` once to sign in through the browser. The token is stored in the OS keyring and refreshed automatically. `GAFFER_NO_OPEN` prints the authorization URL instead of launching a browser. With no OS keyring the token is kept in an encrypted file protected by a passphrase; `GAFFER_KEYRING_PASSWORD` supplies that passphrase where no terminal is available to prompt on (CI, or an editor-spawned process), and without it gaffer fails with guidance rather than hanging.

`gaffer auth --clear` removes every stored token, signing out of all environments. It needs neither the keyring passphrase nor a gaffer project, so it also resets a keyring whose passphrase has been forgotten.

### `quirks_version`

```toml
quirks_version = "26.1.0"
```

Target KurrentDB version (`MAJOR.MINOR.PATCH`). Gaffer turns off engine quirks that have been fixed in the named version or later. Unset means gaffer reproduces every known KurrentDB quirk.

The `GAFFER_QUIRKS_VERSION` environment variable overrides every `quirks_version` in the file. Useful for CI matrices.

Optional.

### `compilation_timeout` / `execution_timeout`

```toml
compilation_timeout = 5000
execution_timeout = 5000
```

Time limits in milliseconds. `compilation_timeout` bounds projection compilation. `execution_timeout` bounds each handler invocation. The runtime applies a 5000ms default for both when omitted.

Per-projection overrides via `execution_timeout` inside `[[projection]]`.

### `telemetry`

```toml
telemetry = false
```

Project-level telemetry opt-out. Setting `false` disables telemetry for any gaffer command run inside this project, regardless of the user's own opt-out state.

For user-level opt-outs that apply across every project, see [Telemetry](../cli/index.md#telemetry).

Optional. Telemetry is on by default.

## Per-projection keys

Each projection is a `[[projection]]` table-array entry.

### `name`

```toml
name = "order-count"
```

Lookup key for `gaffer dev <name>`, `gaffer info <name>`, the VS Code lens, and MCP tools. Required. Names must be unique within a project.

### `entry`

```toml
entry = "projections/order-count.js"
```

Path to the projection's JavaScript source file, relative to the project root. Required.

### `fixtures.<name>`

```toml
fixtures.happy = "fixtures/orders.json"
fixtures.full = "fixtures/orders-full.json"
```

Named JSON events files. Run with `gaffer dev <name> --fixture happy`. Path is relative to the project root.

Optional. Add one entry per scenario you want to re-run.

### `engine_version`

```toml
[[projection]]
name = "order-count"
entry = "projections/order-count.js"
engine_version = 2
```

Projection engine version: `1` or `2`. Required on every projection. V1 is for legacy compatibility with older KurrentDB releases; V2 is the default for new projections written by `gaffer scaffold`. There is no top-level fallback, so each projection states its own version.

### `track_emitted_streams`

```toml
[[projection]]
name = "order-events"
entry = "projections/order-events.js"
engine_version = 1
track_emitted_streams = true
```

Records the streams a projection emits to, mirroring the KurrentDB V1 projection option of the same name. Bool, optional, and valid only when the projection's `engine_version = 1`. Setting it on a V2 projection is a config error.

### `quirks_version` (per-projection)

```toml
[[projection]]
name = "v26-only"
entry = "projections/v26-only.js"
quirks_version = "26.1.0"
```

Per-projection override of the top-level `quirks_version`. Optional.

### `execution_timeout` (per-projection)

```toml
[[projection]]
name = "slow-projection"
entry = "projections/slow-projection.js"
execution_timeout = 30000
```

Per-projection override of the top-level `execution_timeout`. Use for projections with long-running handlers (large reductions, heavy regex work). Optional.

## Resolution order

Settings that exist at both top-level and per-projection resolve from most-specific to least:

| Setting               | Resolution                                                           |
| --------------------- | -------------------------------------------------------------------- |
| `engine_version`      | Per-projection only. Required on each `[[projection]]`.                   |
| `quirks_version`      | `GAFFER_QUIRKS_VERSION` env > per-projection > top-level > unset.         |
| `execution_timeout`   | Per-projection > top-level > 5000ms.                                      |
| `compilation_timeout` | Top-level only > 5000ms.                                                  |
| `connection`          | `--connection` flag > selected env (`--env`, or the default env).         |
