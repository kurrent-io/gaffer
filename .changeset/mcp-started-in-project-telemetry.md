---
"@kurrent/gaffer": patch
---

`gaffer mcp` usage telemetry now records a `started_in_project` flag, distinguishing sessions launched inside a project from project-less ones (for example a globally-registered server started outside any project).

Manifest features are now also recorded for sessions that resolve their project lazily mid-run, for example after the `init` tool creates one. Previously those sessions left `manifest_features_used` unset.
