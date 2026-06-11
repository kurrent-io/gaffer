---
"@kurrent/gaffer": patch
---

`get_timeline` no longer fails with a raw `SQL logic error: no such table: steps` after a live `run`. The in-memory history store now pins itself to a single connection, so concurrent inserts from a live subscription and timeline queries always see the same database. When a session recorded no steps, `get_timeline` now reports "No timeline recorded for this session." instead of an empty range.
