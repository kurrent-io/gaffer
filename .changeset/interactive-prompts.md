---
"@kurrent/gaffer": patch
---

`gaffer init`, `gaffer scaffold`, and `gaffer dev` now prompt interactively when run on a terminal, asking only for values not already supplied as flags or positionals.

- `gaffer init` prompts for the engine version and gains an `--engine-version <1|2>` flag (default `2`).
- `gaffer scaffold` prompts for the path (when omitted) plus source, partitioning, and emit, offering only partitioning options valid for the chosen source.
- `gaffer dev` prompts for the projection (when omitted) and the event source when none is given via `--events` / `--fixture` / `--connection`.
- `gaffer scaffold` and `gaffer dev` gain `--yes` / `-y` to skip prompts (the projection path / name must then be supplied as arguments). On `gaffer init`, `-y` now skips the prompt and uses the default engine version, rather than being a no-op.
- `gaffer scaffold` now rejects per-stream partitioning on a single-stream source up front, instead of generating a projection that only fails when run.

Piped and non-interactive (CI) invocations are unchanged: they never prompt, so existing scripts keep working.
