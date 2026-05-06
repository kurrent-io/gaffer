# Gaffer

Projection toolkit for [KurrentDB](https://kurrent.io). Develop, test, debug, and deploy projections.

**Status:** Early development

## What is Gaffer?

Gaffer is a CLI and runtime for working with KurrentDB projections. It lets you:

- Run and debug projections locally
- Test projections against fixture data or live streams
- Deploy projections to KurrentDB
- Manage projection lifecycle across environments

## Getting started

### Prerequisites

- [DevPod](https://devpod.sh/) or a devcontainer-compatible tool
- Or manually: .NET 10, Go 1.26+, Node 22+, [just](https://just.systems)

### Setup

```sh
git clone https://github.com/kurrent-io/gaffer.git
cd gaffer
just init
```

### Build and test

```sh
just build    # build all projects
just test     # run all tests (99 C# + 9 Go FFI)
just check    # check formatting and linting
just fix      # auto-fix formatting and lint issues
just clean    # remove build artifacts
```

## Project structure

| Path | What |
|------|------|
| [runtime/](runtime/) | C# projection runtime (Jint-based JS execution, NativeAOT shared library) |
| [bindings/go/](bindings/go/) | Go bindings for the runtime (cgo) |

## How it works

The **runtime** executes KurrentDB projection JavaScript locally using [Jint](https://github.com/sebastienros/jint), the same JS interpreter KurrentDB uses. It supports the full projection API: `fromAll`, `fromStream`, `fromCategory`, `when`, `foreachStream`, `partitionBy`, `emit`, `linkTo`, biState, and more.

The runtime builds as a NativeAOT shared library with a C API, allowing it to be embedded in any language via FFI. The **Go bindings** wrap this C API for use by the CLI and Go test libraries.

## License

TBD
