---
"@kurrent/gaffer": patch
---

`gaffer manifest` cleanups:

- The command is now hidden from `gaffer --help`. Its audience is editor extensions and other wrappers that feature-gate their UI against a specific gaffer build, not interactive users.
- The manifest now walks the full command tree and emits nested commands under composite keys (e.g. `config telemetry status`). Previously only direct children of the root appeared, so the `config` subtree was missing from the output. Non-runnable group commands (e.g. bare `config`) are traversed but not emitted: the manifest lists invocable commands, not navigation nodes.
