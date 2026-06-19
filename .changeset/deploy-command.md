---
"@kurrent/gaffer": patch
---

`gaffer deploy` creates or updates projections on an environment from `gaffer.toml`: it creates the ones not yet on the server, updates the ones whose definition changed, and skips the ones already in sync (matched by content hash). The emit flag is always sent explicitly, so an update never clears it.

With no argument it deploys every projection in `gaffer.toml`; name one to deploy just it. A change to engine version or track-emitted-streams can't be applied in place (it would mean recreating the projection and dropping its state), so `gaffer deploy` reports it and leaves the projection untouched. On a terminal it shows live per-projection progress; pass `--json` for machine-readable output.
