# Gaffer

Projection toolkit for KurrentDB. Develop, test, debug, and deploy projections.

## Accuracy

Gaffer must match KurrentDB projection behaviour 1:1, bug:bug where possible. No defaulting values, no swallowing errors, no convenience that hides reality. If a test passes locally with gaffer, it should pass in production.

## Structure

```
runtime/                   # C# (.NET 9) - projection runtime
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
```

## Build

Uses devcontainer (.NET 9, Go, Node 22).

```
just build                 # build all
just test                  # test all
just check                 # check formatting
just runtime publish       # build NativeAOT shared library
just bindings go test      # run Go FFI tests (requires runtime publish)
```

## Runtime

The runtime is a puppetable projection engine. Callers feed JS source and events,
register callbacks, and query state. It does not do I/O or connect to KurrentDB -
that is the caller's responsibility.

Key types: `ProjectionSession`, `ProjectionEvent`, `EmittedEvent`, `QuerySources`.

The C API (gaffer.h) exposes the same functionality for FFI consumers. Go bindings
wrap the C API via cgo.

## Conventions

- C#: .editorconfig matching KurrentDB (tabs, K&R braces, file-scoped namespaces)
- Go: golangci-lint with all linters enabled, goimports + gofumpt formatting
