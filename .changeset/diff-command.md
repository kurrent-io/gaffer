---
"@kurrent/gaffer": patch
---

`gaffer diff <projection>` compares two versions of a projection and reports how they differ. By default it compares the local definition against what's deployed on KurrentDB, reporting its state: in sync, drifted, not deployed, untracked, or invalid. `--left` and `--right` compare any two versions instead. Each is `local`, `deployed`, or a content-hash prefix from `gaffer history` (resolving a hash costs a history read). A version-to-version diff is a pure source diff with no state verdict.

**The source diff renders in-process** rather than shelling out to an external viewer. Every line of both sides is shown with the changes marked in place: dual line-number gutters, +/- colouring, and the span that changed within a line highlighted. The diff is computed on the same canonical form as the drift verdict, so it always matches the `+N -M` stat that `gaffer diff` and `gaffer status` report. It works without git installed, when piped, and in CI. Set `GAFFER_EXTERNAL_DIFF` to open an external viewer instead (e.g. `git diff`, `delta`, `difft`); it is no longer the default path.

**The drift verdict is attributed** from the tool metadata gaffer stamps on deploy, matching `gaffer status`: a projection on the server but not in local config reads as `orphan` or `untracked` (naming the deploying tool where known), and one that differs from what's deployed reads as `local ahead` or `changed externally`. `gaffer diff` also shows the deploy provenance behind the projection: when it was deployed, the tool and version, the deployer, and the source revision.

**A projection that fails to compile no longer aborts the command.** `gaffer diff` still shows the source diff, engine version, and track-emitted-streams, marking `emit` unknown because deriving it needs a successful compile. It exits 0, and the compile error is shown so you know what to fix.

Pass `--json` for machine-readable output. It carries the two sides as `left` and `right`, each with its `ref`, content `hash`, and canonical `source`. A structured `lines` array gives each row a kind (`equal`, `removed`, or `added`), per-side line numbers, and the changed intraline span. On the default deployed-vs-local diff it adds a `verdict` with the drift state, plus the `owner` and `attribution` fields `gaffer status --json` reports.
