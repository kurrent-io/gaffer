---
"@kurrent/gaffer": patch
---

The KurrentDB Go client is bumped to v1.4.1, fixing a nil-pointer panic when listing projections. Projection status reads go through this path; the client could dereference a nil stream on a failed request or a statistics frame with no details, which the status read caught as an unexpected failure. The client now returns a clear error in both cases instead of panicking.
