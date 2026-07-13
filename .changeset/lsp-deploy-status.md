---
"@kurrent/gaffer": patch
---

The language server now serves per-environment deployment status for `gaffer.toml`. On open, save, or a `gaffer/refreshStatus` request it reads each environment's projection drift and runtime state in-process, the same read path as `gaffer status`. It then emits a CodeLens above each `[env.<name>]` block: a roll-up of how the environment's projections compare to local config, a sign-in action when the environment needs authentication, or a muted note when the read can't complete. Editors that consume the language server (starting with the VS Code extension) surface this without reimplementing the fetch.
