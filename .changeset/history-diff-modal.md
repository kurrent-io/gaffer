---
"@kurrent/gaffer": patch
---

`gaffer history` gains `d`: a diff of the selected entry against the version before it, shown in an overlay on the timeline. It answers "what changed at this entry" the way `git show` does: the previous *content* version is the baseline (state changes are skipped - their definition is identical), the first version diffs from empty, and a state-change entry reports "no definition change".

The diff uses the same aligned renderer and tints as `gaffer diff`, with any engine version, emit, or tracking change named above it. The arrow keys keep scrubbing the timeline underneath, so the diff re-renders entry by entry - walking a definition's evolution in place. `PgUp`/`PgDn` scroll a long diff; `esc`, `d`, or `q` closes back to the timeline. A baseline on an older page is fetched automatically.
