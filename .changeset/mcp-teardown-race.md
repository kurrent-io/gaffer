---
"@kurrent/gaffer": patch
---

`gaffer mcp` no longer crashes when a session is torn down while a tool call is in flight. Concurrent tool calls that race a session teardown (for example `stop` while a `run` is parked at a breakpoint) previously could panic the whole MCP server or use-after-free the native session. Teardown is now serialised, a parked handler whose session was stopped returns a clean "session was stopped" error, and any residual handler panic is reported as a tool error instead of taking the process down.
