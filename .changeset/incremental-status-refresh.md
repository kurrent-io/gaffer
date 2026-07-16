---
"@kurrent/gaffer": patch
---

The language server keeps the `gaffer.toml` deployment-status lenses live without recomputing drift on every refresh. When a client polls for freshness it refreshes only live runtime state (running, stopped, faulted) with a cheap read, reusing the cached drift verdict. The verdict is recomputed only when a drift input actually changed.

A local change is caught by file watching: the config saved, or a projection's source file edited. A server-side change is caught by a subscription to each projection's definition stream, so a deploy from outside the editor (the CLI, CI, or another tool) is reflected the moment it lands. The subscriptions are held only for open `gaffer.toml` files, and the timer borrows the same connection for its runtime read instead of dialing every tick.
