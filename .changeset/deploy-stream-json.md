---
"@kurrent/gaffer": patch
---

`gaffer deploy --json --stream` streams the apply as newline-delimited JSON instead of buffering a single array until the run finishes. Each line is a `type`-tagged event. A `deploy_start` fires as each projection's RPC begins, and a `deploy_result` as it settles - the same per-item shape `--json` already emits (`outcome` plus the `recreate`/`faulted`/`emittingReset`/`externalChangeTool` flags and any `error`). A terminal `deploy_summary` then counts the outcomes, so a consumer can render progress live rather than waiting for the whole deploy.

- `--stream` is for the apply: it requires `--json` and can't be combined with `--dry-run` (the preview stays `--dry-run --json`, the one-shot plan envelope).
- stdout stays strictly NDJSON. A pre-apply invalid-plan refusal streams the invalid projections then a `deploy_summary` reporting nothing applied, and a run with nothing to deploy emits a single zeroed `deploy_summary`, so a streaming consumer always ends on one.
- A broken output stream (a disconnected consumer) never aborts an in-flight deploy: emitting goes quiet after the first write error and the apply runs to completion.
