---
"@kurrent/projections-testing": minor
"@kurrent/gaffer-runtime": minor
"@kurrent/gaffer": minor
---

The quirks-selecting option and the quirk registry are renamed to retire the misleading "DB version" / "bug" framing.

- **`dbVersion` is now `quirksVersion`** across the runtime, the JS bindings (`SessionOptions`), and the testing library (`ProjectionOptions`). The value is unchanged: a `MAJOR.MINOR.PATCH` string, where unset still reproduces every known quirk and a set version turns off quirks fixed upstream as of it. Only the key moves. `dbVersion` read as passive info when it actively selects which quirks to emulate, and it collided with `engineVersion`.
- **`knownBugs()` is now `knownQuirks()`**, and **`KnownBug` is now `KnownQuirk`**, in the JS and Go bindings. Most registry entries are deliberate KurrentDB quirks gaffer reproduces, not bugs to report upstream.
- **CLI**: the `gaffer.toml` key `db_version` is now `quirks_version`, the env var `GAFFER_DB_VERSION` is now `GAFFER_QUIRKS_VERSION`, and the MCP resource `gaffer://docs/db-version-bugs` is now `gaffer://docs/quirks`. The connected-server-version telemetry (the `db_version` event property) is unaffected, since it genuinely reports the connected DB version.

No deprecation period: pre-1.0, hard break. An old `dbVersion` or `db_version` key is silently ignored rather than rejected, so update existing call sites and `gaffer.toml` files.
