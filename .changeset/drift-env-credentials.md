---
"@kurrent/gaffer": patch
---

The `[database_config]` drift check now works with `.env`-supplied credentials, and a failed check is visible instead of silent. The node-options read only authenticated from connection-string userinfo. A login kept in `.env`/`.env.<env>` (the recommended secret handling) left the read anonymous, and the failure read as a false "no drift". Credentials now resolve exactly like the main connection's. When the node's options can't be read (auth refusal, no HTTP surface), `status` and `deploy` warn that the check couldn't run, the JSON envelopes carry `configDriftError`, and the MCP deploy confirmation notes the unchecked config.
