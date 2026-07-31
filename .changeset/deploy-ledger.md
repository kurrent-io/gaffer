---
"@kurrent/gaffer": patch
---

Gaffer now records tool metadata on every projection it writes, so a projection carries who changed it and from where: the tool (`Gaffer`) and version, the operation, the source revision, and the acting identity. It follows a shared convention that other KurrentDB tools can write and display, and it's what `gaffer status`, `gaffer diff`, and `gaffer history` read to attribute a change.

Every mutating command stamps its operation: `gaffer deploy` records `deploy`, `gaffer recreate` records `recreate`, and `gaffer rollback` records `rollback`. A recreate is therefore attributed to gaffer rather than appearing as anonymous lifecycle steps.

- **`revision`** defaults to the project's git commit (suffixed `+changes` when the working tree is dirty); set `GAFFER_REVISION` in CI to record the canonical commit.
- **`actor`** defaults to the identity gaffer connects as (the basic-auth user or OAuth client), omitted for an anonymous connection; set `GAFFER_ACTOR` in CI to record the pipeline identity.

The metadata rides on the projection's definition event and is best-effort: against a KurrentDB that predates the feature it is silently ignored and the command behaves exactly as before.
