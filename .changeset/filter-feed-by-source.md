---
"@kurrent/projections-testing": minor
---

`feed()` and `run()` now skip events whose stream the projection's declared source does not subscribe to, matching what real KurrentDB would deliver. A `fromStream("s-1")` projection fed an event on `s-2` previously processed it; it now returns `{ status: "skipped", reason: "wrong-stream" }`. `fromStreams`, `fromCategory` (by stream prefix), and `fromAll` (everything passes) follow the same rule. This closes a footgun where unit tests passed against events the projection would never see in production.
