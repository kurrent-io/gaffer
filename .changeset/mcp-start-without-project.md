---
"@kurrent/gaffer": patch
---

`gaffer mcp` now starts even when there is no `gaffer.toml` in the working directory, instead of failing during the MCP handshake. This makes the server safe to install as a global plugin, where the launch directory is arbitrary.

- The documentation resources (`projection-api`, `gotchas`, `examples`, `quirks`) and `get_version` work without a project.
- Project-dependent tools (`run`, `validate`, `list_projections`, `scaffold`, `get_projection_info`, `list_events`, debug) return a tool error pointing at `gaffer init` rather than taking the server down.
- The project is resolved lazily, so creating a `gaffer.toml` mid-session is picked up on the next tool call without restarting the server.

A `gaffer.toml` that exists but fails to parse or validate still surfaces as a startup error.
