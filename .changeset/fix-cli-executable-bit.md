---
"@kurrent/gaffer": patch
---

Restore the executable bit on the CLI binary inside each per-platform native package. `actions/upload-artifact@v4` stores artifacts as zip, which drops unix permission bits - 0.1.1 shipped `gaffer` as `0644` so `npx @kurrent/gaffer` failed with `EACCES`. The runtime shared libraries (`.so` / `.dylib` / `.dll`) are unaffected; they load via dlopen and don't need `+x`. Windows resolves executability by `.exe` extension, so the win32 binary is also unaffected.
