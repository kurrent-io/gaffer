---
"@kurrent/gaffer": minor
---

**Breaking:** `gaffer.toml` now models connections as named environments, and `engine_version` is set per projection. Top-level `connection` and top-level `engine_version` are no longer supported; loading a file with either fails with a migration hint.

To migrate, move the top-level `connection` into an `[env.<name>]` block (mark one `default = true`), and set `engine_version` on each `[[projection]]`:

```toml
# before
connection = "kurrentdb://localhost:2113?tls=false"
engine_version = 2

[[projection]]
name = "order-count"
entry = "projections/order-count.js"

# after
[env.local]
connection = "kurrentdb://localhost:2113?tls=false"
default = true

[[projection]]
name = "order-count"
entry = "projections/order-count.js"
engine_version = 2
```

Each `[env.<name>]` carries its own `connection`, and exactly one may set `default = true` (used when `--env` is omitted). Environment names must match `^[A-Za-z0-9_-]+$`.

- `gaffer dev` gained `--env <name>` to select an environment; `--connection` is an ad-hoc override that beats both `--env` and the configured environment. The MCP `list_events` and live `run` tools take the same `env` argument.
- A per-environment `.env.<env>` file overlays the base `.env`, so each environment can carry its own credentials. The precedence, highest first, is the shell environment, then `.env.<env>`, then the base `.env`. Both `${VAR}` references in a connection and the `KURRENTDB_USERNAME` / `KURRENTDB_PASSWORD` credentials resolve from those sources.
- `gaffer init` no longer takes `--engine-version` or `--yes`; it writes a commented starter template.
