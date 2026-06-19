---
"@kurrent/gaffer": patch
"@kurrent/gaffer-runtime": patch
"@kurrent/projections-testing": patch
---

Projection info now reports whether a projection writes events. `ProjectionInfo` gains an `emitsEvents` flag, true when the projection calls `emit`, `linkTo`, `linkStreamTo`, or `copyTo`. It is detected on every compile from the source, so consumers no longer need to inspect shape counts.

`gaffer info` shows it ("Emits events: yes"); `gaffer info --json` and the MCP `get_projection_info` and `validate_projection` tools include `emitsEvents`; the testing library exposes it as `info.settings.emitsEvents`.
