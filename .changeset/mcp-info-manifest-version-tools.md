---
"@kurrent/gaffer": patch
---

MCP server gains three read-only introspection tools that mirror the CLI:

- `get_projection_info` returns the same JSON shape as `gaffer info <name> --json` (parsed structure, sources, partition mode, emit declarations, effective engine version). The projection `name` is optional when the project defines exactly one projection.
- `get_manifest` returns the same JSON shape as `gaffer manifest`, so agents can discover which subcommands and flags this gaffer build supports.
- `get_version` returns the gaffer CLI version string.

All three are sync, no session state, and don't take a configured KurrentDB connection.
