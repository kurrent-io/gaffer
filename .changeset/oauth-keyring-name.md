---
"@kurrent/gaffer": patch
---

The OAuth token store honours a new `GAFFER_KEYRING_NAME` environment variable: when set, the encrypted-file fallback lives at `<user-config>/keyring-<name>` instead of the shared default, isolating a client's store on a host with no OS keyring. The name is sanitized to a single safe path segment; the OS-keyring path is unaffected.
