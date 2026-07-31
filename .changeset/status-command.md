---
"@kurrent/gaffer": patch
---

`gaffer status` shows the runtime state of projections on an environment and how they compare to local config: running, stopped, aborted or faulted, progress, and whether each is in sync, drifted, not deployed, untracked, or invalid. With no argument it lists every local and deployed projection as a table; name a projection for its detail. Pass `--json` for machine-readable output.

**Ownership and drift are attributed** from the tool metadata gaffer stamps on deploy, so the table says more than `untracked` or `drifted`:

- A projection on the server but not in local config reads as `orphan` (gaffer deployed it, now gone from `gaffer.toml` - a deletion candidate) or plain `untracked`, with the deploying tool named when its metadata is present. `--json` reports this as `owner`, including `foreign` for a projection another tool manages.
- An in-config projection that differs from what's deployed reads as `local ahead` (you've edited local since your deploy) or `changed externally` (a tool or a direct write changed the server since). `--json` splits the latter into `changed-by-tool` and `changed-server`.

The table gains **LAST DEPLOY** and **DEPLOYED VIA** columns, and naming a projection shows the deploy provenance behind it: when, the tool and version, the deployer, and the source revision. The last-deploy date comes from the event itself, so it shows even for a projection with no tool metadata.

**An aborted projection** reports its own `aborted` runtime state, distinct from a clean `stopped`. An aborted projection was stopped without a final checkpoint, so resuming it reprocesses from the last checkpoint written, re-emitting for an emitting projection. The state surfaces everywhere `runtime.state` appears: the status table (with a warning tint), the detail block, `--json`, and the `deploy_status` MCP tool. The signal is transient - KurrentDB reports it only while it holds the projection in memory, so it reverts to `stopped` after a server restart, and the absence of `aborted` is not proof of a clean pause.

**A projection that fails to compile no longer aborts the command.** A compile error is a per-projection condition: `gaffer status` shows the broken projection as `invalid` and still renders the rest of the table with their real runtime state and drift. It exits 0, and the compile error is shown so you know what to fix.

`gaffer status --json` emits an object: a `projections` array plus a `configDrift` array when the target diverges from `[database_config]`. Each projection carries `owner`, `attribution`, a top-level `lastDeployed` timestamp, `lastWrite` (the tool, its version, the source revision, and the actor), and the deployed definition's content `hash`. The report also names where it landed - the resolved `env`, `target` server, and `production` tier - so it's self-describing without a second call.

Against a KurrentDB without the deploy-metadata field, the attribution degrades to plain in-sync / drifted / untracked.
