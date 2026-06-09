---
"@kurrent/gaffer": patch
---

`.env` is now loaded into the process environment at startup, so a project `.env` applies on every code path, not only after a database connection is made.

- Env-var opt-outs (`GAFFER_TELEMETRY_OPTOUT`, `KURRENTDB_TELEMETRY_OPTOUT`, `DO_NOT_TRACK`, `GAFFER_NO_UPDATE_CHECK`) set in `.env` are now honoured. Previously they were read only from the shell environment.
- The `connection` string in `gaffer.toml` supports `${VAR}` expansion (braced form only), so credentials can stay out of the committed file. An undefined variable is an error; a bare `$` is left untouched.
- The shell environment wins over `.env`: a value already set in the shell, or injected by CI, is never overwritten.
