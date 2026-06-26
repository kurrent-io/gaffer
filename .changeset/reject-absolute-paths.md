---
"@kurrent/gaffer": minor
---

**Breaking:** `gaffer.toml` now rejects absolute `entry` and `fixtures.<name>` paths at load time. Previously an absolute path (e.g. `entry = "/etc/passwd"`, or a Windows drive-letter form like `C:\...`) slipped past validation while the scaffold write path already rejected it. Both surfaces now enforce the same rule: paths must be relative to the project root and must not escape it.
