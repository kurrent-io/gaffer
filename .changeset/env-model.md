---
"@kurrent/gaffer": minor
---

`gaffer.toml` now models connections as named environments, and `engine_version` is set per projection. Loading an old-model file (top-level `connection` or top-level `engine_version`) fails with a migration hint.

- Connections live in `[env.<name>]` blocks, each with a required `connection` and an optional `default = true`. Exactly one environment may be the default. Environment names must match `^[A-Za-z0-9_-]+$`.
- `engine_version` (`1` or `2`) is required on every `[[projection]]`. There is no top-level fallback.
- `gaffer dev` gained `--env <name>` to select an environment. With a default set, `--env` is optional; with none, it is required. `--connection` is an ad-hoc override that beats both `--env` and the configured environment.
- A per-environment `.env.<env>` file overlays the base `.env`. The precedence, highest first, is the shell environment, then `.env.<env>`, then the base `.env`.
- `gaffer init` no longer takes `--engine-version` or `--yes`; it writes a commented starter template. `gaffer scaffold` gained `--engine-version` (`1` or `2`, default `2`) and prompts for it interactively.
