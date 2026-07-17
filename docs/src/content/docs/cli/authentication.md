---
title: Authentication
description: Connect gaffer to KurrentDB with no auth, basic credentials, an X.509 user certificate, or OAuth/OIDC bearer tokens.
---

gaffer connects to KurrentDB through an environment's connection string, declared in [`gaffer.toml`](../reference/gaffer-toml.md#envname). KurrentDB supports four ways to authenticate, and gaffer reads the credentials for each from the connection string and the environment, so secrets stay out of the committed file.

For the full `[env.<name>]` field reference, see the [gaffer.toml reference](../reference/gaffer-toml.md#envname).

## Connection string

Each environment names a KurrentDB connection:

```toml
[env.local]
connection = "kurrentdb://localhost:2113?tls=false"
default = true
```

The `kurrentdb://` scheme connects to a single node; `kurrentdb+discover://` resolves a cluster through gossip. TLS is on by default. `tls=false` disables it, and `tlsVerifyCert=false` skips certificate verification, useful against a self-signed development cluster. gaffer gives up after two node-discovery attempts before reporting an environment unreachable, faster than the KurrentDB client's own default of ten; raise it with `maxDiscoverAttempts` in the connection string when a slow or large cluster needs longer to resolve. Select an environment with `gaffer dev --env <name>`, or let the interactive prompt pick one.

## Credentials and secrets

A connection string can reference environment variables with `${VAR}`, so a password never has to be committed:

```toml
[env.staging]
connection = "kurrentdb://admin:${DB_PASSWORD}@staging:2113"
```

Variables resolve, highest precedence first, from the shell environment, then a per-environment [`.env.<env>`](./index.md#environment-file-env) file, then the base [`.env`](./index.md#environment-file-env) file at the project root. A per-environment file lets each target carry its own credentials, and a value set in your shell or by CI is never overwritten by either file. A referenced variable that isn't set is an error, so a missing secret fails fast rather than connecting without it.

## No authentication

An insecure KurrentDB node accepts connections without credentials. Disable TLS in the connection string and provide nothing else:

```toml
[env.local]
connection = "kurrentdb://localhost:2113?tls=false"
```

This is the default for a local development node started without security.

## Basic authentication

Basic authentication uses a username and password. gaffer reads them from `KURRENTDB_USERNAME` and `KURRENTDB_PASSWORD`, falling back to any `user:password@` in the connection string. Declare the environment with no inline credentials:

```toml
[env.staging]
connection = "kurrentdb://staging:2113"
```

Supply the credentials through a per-environment `.env` file:

```sh title=".env.staging"
KURRENTDB_USERNAME=admin
KURRENTDB_PASSWORD=changeit
```

LDAP authentication is resolved by the server and looks identical to the client, so it uses this same path. Kurrent Cloud managed clusters are basic-auth only.

## Client certificate

An X.509 user certificate authenticates with a certificate and private key instead of a password. Set both files on the environment:

```toml
[env.staging]
connection     = "kurrentdb://staging:2113?tls=true"
user_cert_file = "certs/user.crt"
user_key_file  = "certs/user.key"
```

Both files must be set together, and the certificate is presented in the TLS handshake, so the connection must use TLS. The paths support `${VAR}` expansion and resolve relative to the project root. A client certificate is independent of OAuth, so an environment may use both. See [`user_cert_file` / `user_key_file`](../reference/gaffer-toml.md#envname) for details.

## OAuth / OIDC

OAuth authenticates with a bearer token from an identity provider. Add an [`[env.<name>.oauth]`](../reference/gaffer-toml.md#envnameoauth) block with the issuer and client ID:

```toml
[env.staging]
connection = "kurrentdb://staging:2113?tls=true"

[env.staging.oauth]
issuer    = "https://idp.example.com/realms/kurrent"
client_id = "kurrentdb-client"
scopes    = ["openid"]
```

How a token is obtained depends on whether a client secret is set in the environment:

- **For local development**, leave `KURRENTDB_OAUTH_CLIENT_SECRET` unset and run [`gaffer auth --env <name>`](./commands.md#gaffer-auth) once. It signs in through the browser and stores a token that refreshes automatically. The [VS Code extension](../extension/vs-code.md#authentication) offers the same sign-in inline.
- **For CI**, set `KURRENTDB_OAUTH_CLIENT_SECRET`. gaffer uses the non-interactive client-credentials grant, with no browser.

A stored token is bound to the host the environment's connection names, and gaffer only ever sends it there. Environments pointing at the same host share one sign-in, even across projects. An environment naming a different host needs its own `gaffer auth`, even with the same issuer and client ID, so a token obtained for one cluster is never sent to another. Because the binding comes from the connection string, `gaffer auth` needs it to resolve: an unset `${VAR}` or an unparseable connection fails the sign-in before the browser opens.

See [`[env.<name>.oauth]`](../reference/gaffer-toml.md#envnameoauth) for the full field list and keyring behaviour.
