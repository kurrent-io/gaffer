---
"@kurrent/gaffer": patch
---

`gaffer deploy` creates or updates projections on an environment from `gaffer.toml`: it creates the ones not yet on the server, updates the ones whose definition changed, and skips the ones already in sync (matched by content hash). With no argument it deploys every projection in `gaffer.toml`; name one to deploy just it. The emit flag is always sent explicitly, so an update never clears it.

**Plan, then validate, then apply.** Deploy builds the whole plan against the server before touching it, then compiles the projections it would create or update. If any won't run (one that fails to compile, or that compiles but carries errors which would fault on the server), it refuses the run before writing anything, so a bad projection can't leave the earlier ones already applied. `--no-validate` skips the check, deploying the valid projections and refusing the invalid ones individually instead of aborting the whole run.

**The plan preview** lists each projection with its verdict (`create`, `update`, `rebuild`, `refused`, `invalid`, or `failed`) and a dimmed detail column carrying the refusal reason or the failure error in full. In-sync projections stay a count only, so unchanged ones don't drown the signal. It names the target's reported cluster name, flags an update over a currently-faulted projection (the update won't clear the fault), and cautions when a projection's deployed definition was changed outside gaffer since its last deploy, naming the tool that changed it. Gaffer is the canonical source of truth, so it still deploys: the drift is surfaced, not refused.

**Confirmation** follows the plan. `--yes` skips the prompt; without a terminal (or with `--json`) deploy won't apply unconfirmed, so pass `--yes` in scripts. A production target (a server that declares itself production, or an env with `production = true`) gets a louder confirmation and refuses `--no-validate`.

**A changed query is a logic change:** the new code may read already-processed events differently, so the accumulated state could be wrong. By default deploy keeps the checkpoint, applies the update, and flags the change. Pass `--reset-on-logic-change` to rebuild instead: each logic-changed projection is stopped, updated, reset to the beginning, and restarted so it reprocesses from zero. An emitting projection re-emits on a rebuild and may duplicate into its target streams, so the plan warns and points at `gaffer recreate --delete-emitted` for a clean-emit rebuild. A change to engine version or track-emitted-streams can't be applied in place at all; deploy refuses it and points at `gaffer recreate`.

**Machine-readable output.** `--json` emits the per-projection results as an array. `--dry-run` shows the plan and applies nothing; `--dry-run --json` emits a structured envelope carrying a top-level `verdict` of `in-sync`, `deployable`, or `blocked` (what a real deploy would do), the `changes` count, the resolved `env` and `target`, whether the target is `production`, any `[database_config]` divergence (`configDrift` / `configDriftError`), and the per-projection `plan` array. Each item reports its would-be `outcome` plus the flags a structured consumer needs:

- **`recreate`**: the `refused` outcome is an engine-version or track-emitted-streams change needing a recreate, not an invalid definition.
- **`faulted`**: an update over a currently-faulted projection.
- **`emittingReset`**: a rebuild that re-emits.
- **`logicChange`**: a continued logic change, so CI can alert on it.
- **`externalChange`** / **`externalChangeTool`**: the deployed definition was changed out of band, and the tool that changed it.

`--json --stream` streams the apply as newline-delimited JSON instead of buffering a single array until the run finishes. Each line is a `type`-tagged event: a `deploy_start` as each projection's RPC begins, a `deploy_result` as it settles (the same per-item shape `--json` emits), and a terminal `deploy_summary` counting the outcomes, so a consumer can render progress live. `--stream` is for the apply: it requires `--json` and can't be combined with `--dry-run`. stdout stays strictly NDJSON. A pre-apply invalid-plan refusal streams the invalid projections then a `deploy_summary` reporting nothing applied, and a run with nothing to deploy emits a single zeroed `deploy_summary`, so a streaming consumer always ends on one. A broken output stream never aborts an in-flight deploy: emitting goes quiet after the first write error and the apply runs to completion.

**Exit codes** are stable for scripts: `0` succeeded or nothing to do, `1` an error, `2` changes are pending (`--dry-run` only), `3` refused by a guardrail: confirmation was needed but there was no terminal or `--yes`, or `--no-validate` was used against production. Under `--dry-run` the code follows the verdict: `0` in-sync, `2` deployable, `1` blocked. The guardrail exit code `3` also applies to `recreate` and the operate verbs when they can't confirm non-interactively.

Degrades silently against a KurrentDB without the deploy-metadata field: the external-change detection is skipped and deploy behaves as it otherwise would.
