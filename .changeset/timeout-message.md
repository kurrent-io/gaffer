---
"@kurrent/gaffer": patch
---

A live `run` that times out before catching up no longer reports "timed out waiting for breakpoint" when no breakpoint was set. The `run` tool now names the actual condition (catching up to the head of the stream, hitting a breakpoint, or both), reports how many events were processed, and notes that the session is still running so it can be inspected with `get_state` / `get_timeline` or ended with `stop`.
