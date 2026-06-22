---
"@kurrent/gaffer": patch
---

`gaffer deploy` now compiles every projection before sending anything to the server. If any fails to compile, or compiles but carries errors that would fault on the server (such as a quirk that reproduces an upstream engine crash), the whole deploy is refused up front, so a bad projection can't leave the earlier ones already applied. The check runs locally, before connecting, and `--force` skips it.
