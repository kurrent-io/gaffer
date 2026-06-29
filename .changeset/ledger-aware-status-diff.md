---
"@kurrent/gaffer": patch
---

`gaffer status` and `gaffer diff` are now ledger-aware, reading the tool metadata gaffer stamps on deploy to say more than `untracked` or `drifted`.

- **Ownership** of a projection on the server but not in local config: `orphan` (gaffer deployed it, now gone from `gaffer.toml` - a deletion candidate) or plain `untracked`, with the deploying tool named when its metadata is present. `--json` reports this as `owner`, including `foreign` for a projection another tool manages.
- **Drift attribution** of an in-config projection that differs from what's deployed: `local ahead` (you've edited local since your deploy) or `changed externally` (a tool or a direct write changed the server since). `--json` splits the latter into `changed-by-tool` and `changed-server`.

Both surface in the status table and detail and in `gaffer diff`. The status table gains **LAST DEPLOY** and **DEPLOYED VIA** columns, and naming a projection (or running `gaffer diff`) shows the deploy provenance behind it: when, the tool and version, the deployer, and the source revision. The last-deploy date comes from the event itself, so it shows even for a projection with no tool metadata. In `--json`: `owner`, `attribution`, a top-level `lastDeployed` timestamp, and `lastWrite` (the tool and actor). Against a KurrentDB without the deploy-metadata field it degrades to the previous behaviour.
