---
"@kurrent/gaffer": minor
---

**Breaking:** `gaffer.toml` gains a `[database_config]` section for node-level engine settings, and the top-level `compilation_timeout` / `execution_timeout` keys move into it. A file that still sets them at the top level now fails to load, with a message pointing at the new section.

`[database_config]` declares the engine configuration expected on a deployment target:

- `max_state_size` (newly exposed) caps a projection's serialized state in bytes, defaulting to the server's 16 MiB. It is enforced on local runs, so a projection that would exceed the cap faults locally, catching state bloat before deploy.
- `compilation_timeout` and `execution_timeout` are declaration only: gaffer records them for deploy-time configuration checks but does not apply them to local runs, since a wall-clock budget measured on a dev machine isn't comparable to the server's. To bound how long a local projection may run before gaffer treats it as hung, set the `GAFFER_TIMEOUT_MS` environment variable (default 5000ms). A per-`[[projection]]` `execution_timeout` is likewise declaration only and no longer affects local runs.
