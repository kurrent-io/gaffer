---
"@kurrent/gaffer": patch
---

`gaffer diff <projection>` compares two versions of a projection and reports how they differ. By default it compares the local definition against what's deployed on KurrentDB, reporting its state: in sync, drifted, not deployed, untracked, or invalid. `--left` and `--right` compare any two versions instead. Each is `local`, `deployed`, or a content-hash prefix from `gaffer history` (resolving a hash costs a history read). A version-to-version diff is a pure source diff with no state verdict.

When the query differs, the source diff is shown as well. Pass `--json` for machine-readable output. It carries the two sides as `left` and `right`, each with its `ref`, content `hash`, and canonical `source`. A structured `lines` array gives each row a kind (`equal`, `removed`, or `added`), per-side line numbers, and the changed intraline span. On the default deployed-vs-local diff it adds a `verdict` with the drift state.
