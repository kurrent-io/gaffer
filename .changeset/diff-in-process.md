---
"@kurrent/gaffer": patch
---

`gaffer diff` renders the query source diff itself instead of shelling out to an external viewer. Every line of both sides is shown with the changes marked in place: dual line-number gutters, +/- colouring, and the span that changed within a line highlighted. The diff is computed on the same canonical form as the drift verdict, so it always matches the `+N -M` stat that `gaffer diff` and `gaffer status` report. It now works without git installed, when piped, and in CI.

Set `GAFFER_EXTERNAL_DIFF` to open an external viewer instead (e.g. `git diff`, `delta`, `difft`); it is no longer the default path.
