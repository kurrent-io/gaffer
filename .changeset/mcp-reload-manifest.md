---
"@kurrent/gaffer": patch
---

`gaffer mcp` re-reads `gaffer.toml` on each project-dependent tool call instead of caching it for the session. Editing the manifest mid-session (adding a projection, fixing a connection string) is picked up by the next call with no restart; an invalid manifest surfaces a load error rather than silently serving the last good config.
