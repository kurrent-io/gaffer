---
"@kurrent/gaffer-runtime": patch
---

The native-library loader no longer walks up ancestor directories to find a source-tree build unless `GAFFER_RUNTIME_DEV` is set. The ungated walk-up let an install load the first matching library in any ancestor directory, so an ancestor writable by another principal could plant one and run arbitrary native code. Production installs resolve the platform package and are unaffected; loading from a gaffer source checkout now opts in via `GAFFER_RUNTIME_DEV`.
