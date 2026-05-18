# Telemetry

Anonymous usage analytics and crash reporting for gaffer.

CLI / MCP / extension emit JSON envelopes to a Cloudflare Worker we own; worker validates, stitches sessions in D1, translates to PostHog's shape, forwards. PostHog (EU instance) is the analytics backend.

## Layout

```
schemas/
  events.cue   - the four event types gaffer emits, their properties, and
                 supporting enums. "What does gaffer collect?"
  wire.cue     - the envelope and identifier types used to ship events to
                 the worker. "How do I construct a request?"

generated/     - outputs of `just telemetry build` (runs as part of `just init`):
  telemetry.schema.json   (Draft-07 JSON Schema, the worker validates against this)
  events.gen.go           (Go types, package `generated`)
  events.gen.ts           (TypeScript types)

worker/        - (later) Cloudflare Worker that ingests envelopes, stitches
                 sessions, and forwards to PostHog. Lives here so it's
                 colocated with the schema it consumes.
```

Both CUE files share `package telemetry`; the split is for readers, not for downstream consumers. Worker-side concerns (translation to PostHog, person-property lifting, identity merge) belong in `worker/`, not in `schemas/`.

The CUE is self-documenting at the field level (descriptions survive export to jsonschema). For design rationale (why we bucket, why opt-out is the right default, edge-case behaviour) see the public notice at `https://telemetry.gaffer.kurrent.io/`.

## Consumer notes

**Run `just telemetry build` (or `just init` from the repo root) before `pnpm install` or any Go build.** Downstream packages that import from `@kurrent/gaffer-telemetry` or `github.com/kurrent-io/gaffer/telemetry/generated` rely on the generated outputs existing.

**Go discriminated unions are map-typed.** `go-jsonschema` doesn't synthesise tagged union types from JSON Schema `oneOf`, so `Event` and `CommandInvokedProperties` in `generated/` are `map[string]interface{}`. The per-variant structs (`DevCommandInvokedProperties`, `ProjectionShape`, etc.) are properly typed - emit code is expected to construct one of them, marshal to a map, and assign. The CLI's telemetry helpers wrap this so call sites stay typed.

**TS narrowing on `properties.command` works** as a discriminated union (`if (props.command === 'dev') { ... }`). The codegen pipeline includes a small jq pass that hoists `required` from CUE's `& Base` extension allOf so json-schema-to-typescript marks `command` as required - see comment in `justfile`.

## What we collect

Pseudonymous (per-install random id; no account, no PII):

- Which gaffer commands run, with bucketed counts of work done.
- Source-mechanical projection shape (which builtins called, file size tier). Never names, paths, or contents.
- Crash reports from gaffer's own code (paths and user-JS frames removed by construction).

## How to disable

Any one of these silences telemetry:

- `telemetry = false` at the top of `gaffer.toml` (project-level).
- `gaffer config telemetry off` (user-level).
- `GAFFER_TELEMETRY_OPTOUT`, `KURRENTDB_TELEMETRY_OPTOUT`, or `DO_NOT_TRACK` set to any truthy value.
- VS Code's `telemetry.telemetryLevel` (extension respects it).

Full disclosure at `https://telemetry.gaffer.kurrent.io/`.
