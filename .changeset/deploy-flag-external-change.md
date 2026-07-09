---
"@kurrent/gaffer": patch
---

`gaffer deploy` now flags a projection whose deployed definition was changed outside gaffer since its last deploy, so a deploy doesn't silently revert an out-of-band change.

- The plan preview and the non-interactive (`--yes`) apply warnings show a caution ("`<name>` was changed outside gaffer since its last deploy; deploying overwrites it"), naming the tool when another tool made the change.
- `gaffer deploy --json` carries `externalChange: true` on the affected item, alongside `logicChange`, so CI can alert, and `externalChangeTool` names the tool when another tool made the change.

Gaffer is the canonical source of truth, so it still deploys - the drift is surfaced, not refused. Degrades silently against a KurrentDB without the deploy-metadata field.
