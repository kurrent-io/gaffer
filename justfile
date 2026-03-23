# Root justfile orchestrates cross-module dependencies and ordering.
# Sub-module justfiles are standalone - they just run their own commands.

default:
    @just --list

# Install all dependencies and tools
[parallel]
init: runtime::init bindings::init

# Ensure runtime .so is built (sequential: build then publish)
[private]
_runtime: runtime::build runtime::publish

# Build all projects that can build in parallel (after runtime)
[private]
[parallel]
_build: cli::build

# Build everything
build: _runtime _build

# Run all test suites in parallel (after runtime is published)
[private]
[parallel]
_test: runtime::test bindings::test cli::test

# Run all tests
test: _runtime _test

# Check formatting and linting across all projects
[parallel]
check: runtime::check bindings::check cli::check

mod runtime
mod bindings
mod cli
