---
"@kurrent/gaffer": patch
---

`gaffer deploy` builds the full plan first, then validates it. It plans every projection against the server, then compiles the ones it would create or update and refuses the run if any won't run: one that fails to compile, or that compiles but carries errors which would fault on the server (such as a quirk reproducing an upstream engine crash). Refusing before any write means a bad projection can't leave the earlier ones already applied. `--no-validate` skips the check, deploying the valid projections and refusing the invalid ones individually instead of aborting the whole run. A projection that won't run shows in the plan as `invalid`, distinct from a `refused` one that is valid but needs recreating.
