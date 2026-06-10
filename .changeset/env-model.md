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

Also in this release:

- Connections live in `[env.<name>]` blocks, each with a required `connection` and an optional `default = true`. Exactly one environment may be the default, and environment names must match `^[A-Za-z0-9_-]+$`.
- `gaffer dev` gained `--env <name>` to select an environment. A live run uses the default env when `--env` is omitted, so `--env` is only required for a live run when no env is the default (fixture runs need neither). `--connection` is an ad-hoc override that beats both `--env` and the configured environment. The MCP `list_events` and live `run` tools take the same `env` argument.
- A per-environment `.env.<env>` file overlays the base `.env`. The precedence, highest first, is the shell environment, then `.env.<env>`, then the base `.env`. `${VAR}` references in a connection are expanded from those sources.
- `gaffer init` no longer takes `--engine-version` or `--yes`; it writes a commented starter template. `gaffer scaffold` gained `--engine-version` (`1` or `2`, default `2`) and prompts for it interactively.
