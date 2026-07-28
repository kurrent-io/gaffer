---
title: CI/CD integration
description: Run gaffer deploy unattended - the dry-run drift gate, non-interactive confirmations, stable exit codes, and pipeline attribution.
---

`gaffer deploy` is built to run unattended: it plans deterministically, confirms through a flag instead of a prompt, and exits with a stable code a pipeline can branch on.

## The drift gate

The canonical CI pattern is a dry run:

```sh
gaffer deploy --dry-run --env staging
```

Exit `0` means everything is in sync, `2` means there is drift to apply, and `1` means something needs attention (a projection that won't compile, a failed server call). A pipeline gates on that:

```sh
gaffer deploy --dry-run --env staging
case $? in
  0) echo "projections in sync" ;;
  2) gaffer deploy --yes --env staging ;;
  *) exit 1 ;;
esac
```

Run it on every merge and drift never accumulates. The gate is cheap when in sync, and the apply step re-plans from scratch, so nothing is carried between the two calls. Add `--json` for a machine-readable plan; `gaffer deploy --dry-run --json` emits a verdict envelope (`in-sync`, `deployable`, or `blocked`) with the per-projection plan. See [Drift, history and rollback](./drift.md) for what counts as drift.

## Non-interactive mode

Deploy fails closed: without a terminal, a run that would change something refuses to act (exit `3`) rather than applying unconfirmed. `--yes` is the explicit confirmation, so scripts pass it to apply. The guarded operate verbs (`recreate`, `rollback`, `delete`, and `disable` [against production](./production.md)) behave the same way.

`--yes` does not weaken validation: the compile-and-diagnostics preflight still runs, and a production target still refuses `--no-validate`. For live progress instead of a single result, `--stream` emits the apply as NDJSON, one event per line (requires `--json`, not combinable with `--dry-run`).

## Exit codes

The [exit-code contract](../cli/index.md#exit-codes) is stable for scripts:

| Code | Meaning                                                                       |
| ---- | ----------------------------------------------------------------------------- |
| `0`  | Succeeded, or nothing to do.                                                   |
| `1`  | An error: invalid projection, failed server call, or a change needing recreate. |
| `2`  | Changes pending (`--dry-run` only).                                            |
| `3`  | Refused by a guardrail: no confirmation possible, or `--no-validate` on production. |
| `4`  | An environment needs an interactive sign-in.                                   |

## Pipeline attribution

Every create and update lands in the [deploy ledger](./drift.md#the-deploy-ledger-and-history) with an actor and source revision. The defaults (the connection's user, the checkout's git commit) describe the pipeline poorly, so set the [`GAFFER_ACTOR` and `GAFFER_REVISION`](../cli/index.md#common-flags) environment variables. The actor becomes the pipeline identity (`ci@deploys`), and the revision the canonical commit, which matters when the checkout's HEAD is a synthetic PR merge commit.

## Authentication

Exit `4` means the environment wants an interactive sign-in, which CI can't give. Supply credentials through the pipeline instead: `KURRENTDB_USERNAME` / `KURRENTDB_PASSWORD` for basic auth, or `KURRENTDB_OAUTH_CLIENT_SECRET` for an OAuth environment, which switches gaffer to the non-interactive client-credentials grant. See [Authentication](../cli/authentication.md).
