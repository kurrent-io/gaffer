---
"@kurrent/gaffer": patch
---

`gaffer.toml` is now written atomically (temp file + rename) instead of rewritten in place. A reader that re-reads the manifest on change (the LSP file watcher, the MCP server) can no longer catch a half-written file, and a crash mid-write can no longer truncate it.
