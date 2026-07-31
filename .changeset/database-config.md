---
"@kurrent/gaffer": minor
---

**Breaking:** `gaffer.toml` gains a `[database_config]` section for node-level engine settings, and the top-level `compilation_timeout` / `execution_timeout` keys move into it. A file that still sets them at the top level now fails to load, with a message pointing at the new section.

`[database_config]` declares the engine configuration expected on a deployment target:

- `max_state_size` (newly exposed) caps a projection's serialized state in bytes, defaulting to the server's 16 MiB. It is enforced on local runs, so a projection that would exceed the cap faults locally, catching state bloat before deploy.
- `compilation_timeout` and `execution_timeout` are declaration only: gaffer records them for deploy-time configuration checks but does not apply them to local runs, since a wall-clock budget measured on a dev machine isn't comparable to the server's. To bound how long a local projection may run before gaffer treats it as hung, set the `GAFFER_TIMEOUT_MS` environment variable (default 5000ms). A per-`[[projection]]` `execution_timeout` is likewise declaration only and no longer affects local runs.

**`gaffer deploy` and `gaffer status` check the target against the declaration** and warn when the node's live engine settings diverge: one line per differing knob, read from the node's options endpoint. Fixtures and local runs assumed the declared values, so a server enforcing a different `max_state_size` or timeout is visible before it bites. A non-positive `max_state_size` declares the engine default rather than a value.

The node-options read authenticates exactly like the connection itself: connection-string userinfo, credentials from `.env` / `.env.<env>`, an OAuth bearer token (client-credentials or the login stored by `gaffer auth`, never prompting), or the environment's user certificate presented in the TLS handshake, honouring the connection's `tlsCaFile` and `tlsVerifyCert` settings.

The check is advisory, but a failure is visible rather than silent. When the node's options can't be read - no HTTP surface, an auth refusal - `status` and `deploy` warn that the check couldn't run instead of reporting a false "no drift", the JSON envelopes carry `configDriftError`, and the MCP deploy confirmation notes the unchecked config. `gaffer status --json` reports a divergence as a `configDrift` array of `{"knob", "server", "local"}`. `gaffer deploy --json` keeps its warning on stderr, so the stdout payload stays clean while CI logs still show it.
