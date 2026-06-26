---
"@kurrent/gaffer": patch
---

`gaffer deploy` now records tool metadata on every projection it creates or updates, so a projection carries who deployed it and from where: the tool (`Gaffer`) and version, the operation, the source revision, and the acting identity. It follows a shared convention that other KurrentDB tools can write and display.

- **`revision`** defaults to the project's git commit (suffixed `+changes` when the working tree is dirty); set `GAFFER_REVISION` in CI to record the canonical commit.
- **`actor`** defaults to the identity gaffer connects as (the basic-auth user or OAuth client), omitted for an anonymous connection; set `GAFFER_ACTOR` in CI to record the pipeline identity.

The metadata rides on the projection's definition event and is best-effort: against a KurrentDB that predates the feature it is silently ignored and deploy behaves exactly as before.
