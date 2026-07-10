---
"@kurrent/gaffer": patch
---

`gaffer deploy`'s plan preview now lists each projection, not just totals. Every projection that would change shows a verdict - `create`, `update`, `rebuild`, `refused`, `invalid`, or `failed` - and a dimmed detail column carrying the refusal reason or the failure error in full. In-sync projections stay a count only, so unchanged ones don't drown the signal.

`gaffer deploy --dry-run --json` now emits a structured envelope instead of a bare array. It carries a top-level `verdict` of `in-sync`, `deployable`, or `blocked` (what a real deploy would do). Alongside the verdict it reports the `changes` count, the resolved `env` and `target`, whether the target is `production`, any `[database_config]` divergence (`configDrift`/`configDriftError`), and the per-projection `plan` array. Each plan item reports its would-be `outcome`, plus the flags a structured consumer needs:

- **`recreate`**: the `refused` outcome is an engine-version or track-emitted-streams change needing a recreate, not an invalid definition.
- **`faulted`**: an update over a currently-faulted projection.
- **`emittingReset`**: a rebuild that re-emits.
- **`externalChangeTool`**: the tool that changed the deployed definition out of band.

The exit code follows the verdict: `0` in-sync, `2` deployable, `1` blocked.
