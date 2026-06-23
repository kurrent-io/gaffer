---
"@kurrent/gaffer": patch
---

`gaffer recreate <projection>` destroys and rebuilds a deployed projection from local config: stop it, delete it (with its state and checkpoint streams), and create it fresh, reprocessing from zero. It applies a create-only change that deploy can't make in place (engine version, track-emitted-streams), or rebuilds a wedged projection an in-place reset can't fix. The projection is compiled before anything is deleted, so a broken local definition can't leave you with nothing to rebuild; `--no-validate` skips that check (production refuses it). It always confirms, more prominently against production, with `--yes` for non-interactive use. `--delete-emitted` also wipes the emitted streams so the rebuild doesn't re-emit duplicates.
