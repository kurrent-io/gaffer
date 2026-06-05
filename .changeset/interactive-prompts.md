---
"@kurrent/gaffer": patch
---

`gaffer init`, `gaffer scaffold`, and `gaffer dev` now prompt interactively when run on a terminal, asking only for values not already supplied as flags or positionals.

- `gaffer init` prompts for the engine version and gains an `--engine-version <1|2>` flag (default `2`).
- `gaffer scaffold` prompts for the path (when omitted) plus source, partitioning, and emit, then confirms before writing.
- `gaffer dev` prompts for the projection (when omitted) and the event source when none is given via `--events` / `--fixture` / `--connection`.
- `gaffer scaffold` and `gaffer dev` gain `--yes` / `-y` to skip prompts. On `gaffer init`, `-y` now means "skip prompts and accept defaults" rather than being a no-op.

Piped and non-interactive (CI) invocations are unchanged: they never prompt, so existing scripts keep working.
