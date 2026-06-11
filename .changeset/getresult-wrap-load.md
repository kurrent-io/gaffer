---
"@kurrent/gaffer-runtime": patch
---

`getResult` now wraps errors from reloading state (a faulted session, or malformed cached state) as a `ProjectionTransformError`, instead of leaking a raw runtime exception. It now reports state-reload failures the same way `feed` does.
