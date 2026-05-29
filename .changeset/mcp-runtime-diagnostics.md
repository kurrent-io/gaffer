---
"@kurrent/gaffer": patch
---

The MCP server now surfaces the runtime quirks that fired while processing an event, so an assistant can spot a fired quirk and act on it. `get_step` gains a top-level `diagnostics` array of the full quirk objects, and `get_timeline` / `get_history` carry the distinct quirk codes (`quirks`) per step. Each code cross-references the existing `gaffer://docs/quirks` resource, which explains the quirk and names a `quirksVersion` that opts out where one exists.
