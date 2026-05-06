# Contributing to Gaffer

Thanks for your interest in contributing. This document covers how to
get a working development environment, the conventions we use, and the
legal side of contributing code.

## Reporting bugs and proposing features

- File bugs in [GitHub Issues](https://github.com/kurrent-io/gaffer/issues/new/choose).
- Propose features and ask questions in [GitHub Discussions](https://github.com/kurrent-io/gaffer/discussions).

## Development setup

Gaffer uses a devcontainer with .NET 10, Go, and Node 22. The simplest
path is to open the repo with a devcontainer-compatible tool:

- [Visual Studio Code dev containers](https://code.visualstudio.com/docs/devcontainers/containers)
- [DevPod](https://devpod.sh)
- Any other dev container CLI

If you'd rather set up the toolchain manually:

- .NET 10 SDK
- Go 1.26 or later
- Node 22 or later
- [just](https://just.systems)

### Common workflow

```sh
just init               # install dependencies (run once)
just build              # build all
just test               # run all tests
just check              # check formatting and linting
just fix                # auto-fix formatting and lint issues
just clean              # remove build artifacts
```

Other useful targets:

```sh
just runtime publish    # build the NativeAOT shared library
just bindings go test   # run Go FFI tests (requires `runtime publish`)
just db-up              # start KurrentDB for integration tests
just test-integration   # run integration tests
just db-down            # stop KurrentDB
```

## Pull requests

- Branch from `main`.
- Keep history clean. Rebase rather than merge when updating your branch.
- Squash on merge is the default.

### Commit messages

- Sentence case, imperative mood for the subject.
- No trailing period on the subject.
- Short subjects; elaborate in the body if needed.
- No conventional-commit prefixes (no `feat:` / `fix:` / `chore:`).

## Triage

Issues and pull requests are reviewed on a best-effort basis. There's
no SLA. Stale items without activity may be closed after 30 days.

## Licensing and legal rights

By contributing to Gaffer:

1. You assert that the contribution is your original work.
2. You assert that you have the right to assign the copyright for the
   work.
3. You accept the [Contributor License Agreement](https://gist.github.com/eventstore-bot/7a1e56c21e81f44a625a7462403298bf) (CLA) for your contribution.
4. You accept the licence applicable to the file(s) you're modifying
   (see [LICENSE.md](LICENSE.md) and [LICENSE_CONTRIBUTIONS.md](LICENSE_CONTRIBUTIONS.md)).
