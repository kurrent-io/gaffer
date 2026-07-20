---
"@kurrent/gaffer": patch
---

The language server now serves a `gaffer/operateProjection` request: it runs an operate verb (pause/resume/abort/delete) on a projection over the server's warm per-env connection, so editors can operate without spawning a `gaffer` process per verb. The per-projection **Manage...** CodeLens payload also gained each environment's production flag and the projection's runtime state, so the editor can offer pause-vs-resume and pick the right confirmation tier. Editors opt in via the same `statusLens` initialization option as the deployment-status lenses.
