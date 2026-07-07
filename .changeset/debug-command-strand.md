---
"@kurrent/gaffer": patch
---

Racing debug commands can no longer wedge a debug session. When two resume verbs raced on a paused projection (a double-clicked continue, or the MCP auto-step racing a user step), the loser's command could be queued just as the engine resumed. Its caller then blocked forever, and the stale command silently resumed the next breakpoint instead. The runtime now makes the enqueue atomic with the resume, fails commands that lost the race with an error instead of stranding them, and never carries queued commands across a pause.
