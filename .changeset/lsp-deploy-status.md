---
"@kurrent/gaffer": patch
---

The language server now serves per-environment deployment status for `gaffer.toml`. On open, save, or a `gaffer/refreshStatus` request it reads each environment's projection drift and runtime state in-process, reusing the same drift and target reads as `gaffer status`. It then emits a CodeLens above each `[env.<name>]` block. The lens is a roll-up of how the environment's projections compare to local config, a sign-in action when the environment needs authentication, or a muted note when the read can't complete. Editors opt in via an initialization option, so this is a no-op for clients that don't render it. Editors that consume the language server (starting with the VS Code extension) surface this without reimplementing the fetch.
