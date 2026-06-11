---
"gaffer-vscode": patch
---

Exception telemetry now redacts connection-string credentials and hosts from error messages before they leave your machine, extending the filesystem-path scrubbing already applied to these messages. A URL carrying credentials (e.g. `esdb://user:pass@cluster:2113`) is reduced to `esdb://<redacted>`, keeping only the scheme and path.
