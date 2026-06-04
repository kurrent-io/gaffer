# Gaffer

Projection toolkit for KurrentDB. Develop, test, debug, and deploy projections.

## Accuracy

Gaffer must match KurrentDB projection behaviour 1:1, bug:bug where possible. No defaulting values, no swallowing errors, no convenience that hides reality. If a test passes locally with gaffer, it should pass in production.

## Structure

```
runtime/                   # C# (.NET 10) - projection runtime
  Gaffer.Runtime/          # NativeAOT shared library, Jint-based JS execution
  Gaffer.Runtime.Tests/    # xUnit tests
  Gaffer.Sdk/              # Shared types (ProjectionInfo) for runtime and consumers
  include/gaffer.h         # C API header
bindings/
  go/                      # Go bindings (gafferruntime package), FFI tests
  js/                      # JS/TS bindings (@kurrent/gaffer-runtime), koffi FFI
    native/linux-x64/      # Platform-specific native package
cli/                       # Go CLI (Cobra) - dev, info, init, scaffold, config, mcp, lsp, version; hosts the DAP, LSP, and MCP servers
testing/
  js/                      # @kurrent/projections-testing - test lib wrapping runtime
editors/
  vscode/                  # VS Code extension - debug adapter, status panels, LSP client, gaffer.toml support
types/                     # @kurrent/projections-types - TS declarations for projection files (consumed by the VS Code tsserver plugin)
telemetry/                 # Cross-cutting analytics: CUE schemas, generated types, Cloudflare Worker
demo/                      # Example gaffer project with fixtures
docs/                      # Astro + Starlight site published to gaffer.kurrent.io
  src/content/docs/        # User-facing markdown (one file per page)
  public/                  # Static assets served at the site root (downloadable fixtures, favicons, fonts)
specs/                     # Internal protocol / behaviour specifications
assets/                    # Banners and demo GIFs referenced from repo / package READMEs
tools/
  fixtures/                # Shared JSON test fixtures (sources, state, callbacks, etc.)
  kurrentdb/               # Docker compose for integration tests
  vhs/                     # VHS tape scripts for regenerating recordings under assets/
```

## Build

Uses devcontainer (.NET 10, Go, Node 22).

```
just init                  # install dependencies across all modules
just build                 # build all
just test                  # test all
just check                 # check formatting and linting
just fix                   # auto-fix formatting and lint issues across all projects
just clean                 # remove build artifacts across all projects
just runtime publish       # build NativeAOT shared library
just bindings go test      # run Go FFI tests (requires runtime publish)
just db-up                 # start KurrentDB for integration tests
just test-integration      # run integration tests
just db-down               # stop KurrentDB
```

## Runtime

The runtime is a puppetable projection engine. Callers feed JS source and events,
register callbacks, and query state. It does not do I/O or connect to KurrentDB -
that is the caller's responsibility.

Key types: `ProjectionSession`, `ProjectionEvent`, `EmittedEvent`, `ProjectionInfo` (in `Gaffer.Sdk`).

Errors: `ProjectionException` base with 8 typed exceptions (InvalidProjection,
CompilationTimeout, InvalidArgument, ProjectionHandler, ExecutionTimeout,
MalformedEvent, StateSerialization, ProjectionTransform). Formatted messages
built by `ErrorFormatter` with Gleam-style source snippets and event context.

The C API (gaffer.h) exposes the same functionality for FFI consumers.
Fallible functions take a `const char** error_out` out-parameter; on
failure they return NULL/0 and `*error_out` points to structured error
JSON the caller frees via `gaffer_free`. All returned strings are
caller-owned and freed the same way.
Go bindings wrap the C API via cgo with a `Session` struct.
JS bindings use koffi FFI with typed error classes.

## Debugging

The runtime exposes a debug API on `ProjectionSession` when constructed
with `Debug = true`: `SetBreakpoint`/`ClearBreakpoints`,
`Continue`/`Pause`, `StepInto`/`StepOver`/`StepOut`, `GetCallStack`,
`GetScopes`, `GetVariables`, `Evaluate`, and an
`OnBreak: Action<BreakInfo>` callback. Debug types (`BreakInfo`,
`DebugCallFrame`, `DebugScopeInfo`, `DebugVariable`) live in
`runtime/Gaffer.Runtime/Events/`.

The CLI runs a DAP server (`cli/internal/dap`) that adapts this API to
the Debug Adapter Protocol. The VS Code extension is a real debug
adapter that connects to the CLI's DAP server, with breakpoint UI,
call-stack and scope panels, and `Run > Debug` integration tied to
`gaffer.toml` projections.

## LSP server

The CLI hosts an LSP server (`gaffer lsp`, in `cli/internal/lsp`) over
stdio. Capabilities: TextDocumentSync, CodeLensProvider,
WorkspaceSymbolProvider. Parses `gaffer.toml` (only - JS isn't parsed),
emits lenses on the toml plus on each projection's entry JS by matching
the open URI against cached parses. The VS Code extension consumes this
as its source of truth for lensing rather than re-implementing
in-process; other editors connect over stdio the same way.

## MCP server

The CLI hosts an MCP server (`gaffer mcp`, in `cli/internal/mcpserver`)
that exposes projection lifecycle and debug tools to AI assistants:
run, validate, stop, scaffold, get state/step/history/timeline, list
projections and events, debug-continue, step, and evaluate. Breakpoints
are managed via DAP, not MCP. The `demo/.mcp.json` registers the server
for the demo project.

## Telemetry

Anonymous usage analytics and crash reporting. CLI / MCP / extension emit
JSON envelopes to a Cloudflare Worker which validates, stitches sessions
in D1, and forwards to PostHog (EU).

- Schemas in `telemetry/schemas/` (CUE); generated TS / Go / JSON Schema
  in `telemetry/generated/`, regenerated by `just telemetry build` (runs as
  part of `just init`).
- Worker in `telemetry/worker/`.
- Go emitter in `cli/internal/telemetry`.
- JS emitter in `editors/vscode/src/telemetry`.

Opt-out cascade: `telemetry = false` in `gaffer.toml`, `gaffer config telemetry off`,
`GAFFER_TELEMETRY_OPTOUT` / `KURRENTDB_TELEMETRY_OPTOUT` / `DO_NOT_TRACK` env vars,
VS Code's `telemetry.telemetryLevel`. Public notice at
<https://gaffer.kurrent.io/telemetry/>.

## NativeAOT rules

Do not use LINQ extension methods on arrays in runtime code (AsEnumerable, Select, Where, etc.). LINQ interface dispatch on arrays crashes when the .so is loaded by non-.NET FFI hosts (koffi/Node). Use indexed `for` loops instead.

## Conventions

- C#: .editorconfig matching KurrentDB (tabs, K&R braces, file-scoped namespaces)
- Go: golangci-lint with all linters enabled, goimports + gofumpt formatting
- JS/TS (bindings/js, testing/js, editors/vscode): prettier + eslint, tabs
