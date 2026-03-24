# Gaffer

Projection toolkit for KurrentDB. Develop, test, debug, and deploy projections.

## Accuracy

Gaffer must match KurrentDB projection behaviour 1:1, bug:bug where possible. No defaulting values, no swallowing errors, no convenience that hides reality. If a test passes locally with gaffer, it should pass in production.

## Structure

```
runtime/                   # C# (.NET 10) - projection runtime
  Gaffer.Runtime/          # NativeAOT shared library, Jint-based JS execution
  Gaffer.Runtime.Tests/    # xUnit tests
  include/gaffer.h         # C API header
bindings/
  go/                      # Go bindings (gafferruntime package), FFI tests
  js/                      # JS/TS bindings (@kurrent/gaffer-runtime), koffi FFI
    native/linux-x64/      # Platform-specific native package
cli/                       # Go CLI (Cobra) - init, scaffold, dev
testing/
  js/                      # @kurrent/projections-testing - test lib wrapping runtime
demo/                      # Example gaffer project with fixtures
tools/
  fixtures/                # Shared JSON test fixtures (sources, state, callbacks, etc.)
  kurrentdb/               # Docker compose for integration tests
```

## Build

Uses devcontainer (.NET 10, Go, Node 22).

```
just build                 # build all
just test                  # test all
just check                 # check formatting and linting
just format                # auto-fix formatting across all projects
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

Key types: `ProjectionSession`, `ProjectionEvent`, `EmittedEvent`, `QuerySources`.

Errors: `ProjectionException` base with 8 typed exceptions (InvalidProjection,
CompilationTimeout, InvalidArgument, ProjectionHandler, ExecutionTimeout,
MalformedEvent, StateSerialization, ProjectionTransform). Formatted messages
built by `ErrorFormatter` with Gleam-style source snippets and event context.

The C API (gaffer.h) exposes the same functionality for FFI consumers.
`gaffer_get_last_error()` returns structured error JSON on failure.
Go bindings wrap the C API via cgo with a `Session` struct.
JS bindings use koffi FFI with typed error classes.

## NativeAOT rules

Do not use LINQ extension methods on arrays in runtime code (AsEnumerable, Select, Where, etc.). LINQ interface dispatch on arrays crashes when the .so is loaded by non-.NET FFI hosts (koffi/Node). Use indexed `for` loops instead.

## Conventions

- C#: .editorconfig matching KurrentDB (tabs, K&R braces, file-scoped namespaces)
- Go: golangci-lint with all linters enabled, goimports + gofumpt formatting
