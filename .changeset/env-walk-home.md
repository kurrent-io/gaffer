---
"@kurrent/gaffer": patch
---

The startup `.env` auto-load no longer walks above `$HOME` to find the project root. A stray `gaffer.toml` in a shared ancestor (a world-writable `/tmp`, or `/home` on a multi-user host) could otherwise make its `.env` (including `KURRENTDB_USERNAME` / `KURRENTDB_PASSWORD`) ambient for every `gaffer` invocation below it. The walk now stops at `$HOME`, matching the telemetry opt-out walk; the telemetry project-id walk is bounded the same way.
