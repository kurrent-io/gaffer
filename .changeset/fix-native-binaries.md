---
"@kurrent/gaffer-runtime": patch
---

Republish the per-platform native packages with their compiled `gaffer.so` / `.dylib` / `.dll`. 0.1.0 shipped those packages empty due to a CI workflow bug (`upload-artifact@v4` strips directory paths for single-file uploads, so the download step at publish time saw colliding bare-named files at the workspace root instead of files in their per-platform package dirs). Installing 0.1.0 left koffi unable to load the runtime. Reinstall `>=0.1.1` to pick up the fix.
