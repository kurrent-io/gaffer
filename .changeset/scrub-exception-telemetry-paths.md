---
"gaffer-vscode": patch
---

Exception telemetry now strips filesystem paths from error messages before they leave your machine. OS-level errors (e.g. a permission-denied `stat`) embed absolute paths that could include your username; these are now replaced with `<path>`, matching the telemetry notice's existing "no paths or error messages" promise. Stack frames were already scrubbed to basenames.
