---
"@kurrent/gaffer": patch
---

The MCP server gains an `init` tool, so an assistant can create a gaffer project without leaving the protocol. Previously a project-less server could read the docs but had no in-protocol way to bootstrap one.

- `init` creates a `gaffer.toml` in the server's project directory (the `--project` / `GAFFER_PROJECT` override, otherwise the working directory). The projection tools then resolve it on the next call, with no restart.
- It refuses to run when a project is already in scope, naming where one was found, so it never shadows an existing project with a nested copy.
- `gaffer init` and the tool now share one implementation, so they can't drift on what a fresh project looks like.
