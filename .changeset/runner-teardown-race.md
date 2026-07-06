---
"@kurrent/gaffer": patch
---

Session teardown in the debug surfaces no longer races in-flight debug commands. Stopping an MCP run or ending/restarting a DAP debug session could free the native projection session while a step or resume from another goroutine was still executing inside it. That use-after-free could crash the process. The engine runner now refuses new session calls once teardown begins and waits out the in-flight ones before freeing the session.
