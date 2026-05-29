---
"@kurrent/gaffer": patch
---

The `gaffer dev` DAP `gaffer/stats` event now carries a `quirks` count: the number of distinct runtime-quirk codes seen so far in the session. This lets an editor tally fired quirks in its status view without tracking the per-step warnings itself.
