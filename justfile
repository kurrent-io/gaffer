# Root justfile orchestrates cross-module dependencies and ordering.
# Sub-module justfiles are standalone - they just run their own commands.

default:
    @just --list

# Root-level tool setup
[private]
[parallel]
_init-root:
    corepack enable

# Install all module dependencies in parallel
[private]
[parallel]
_init-modules: runtime::init bindings::init testing::init

# Install all dependencies and tools
init: _init-root _init-modules

# Ensure runtime .so is built (sequential: build then publish)
[private]
_runtime: runtime::build runtime::publish

# Build all projects that can build in parallel (after runtime)
[private]
[parallel]
_build: cli::build bindings::build testing::build

# Build everything
build: _runtime _build

# Run all test suites in parallel (after runtime is published)
[private]
[parallel]
_test: runtime::test bindings::test cli::test testing::test

# Run all tests
test: _runtime _test

# Start KurrentDB for integration tests
db-up:
    docker compose -f tools/kurrentdb/docker-compose.yml up -d --wait

# Stop KurrentDB
db-down:
    docker compose -f tools/kurrentdb/docker-compose.yml down

# Run integration tests (requires KurrentDB)
test-integration: testing::test-integration

# Check formatting and linting across all projects
[parallel]
check: runtime::check bindings::check cli::check testing::check

mod runtime
mod bindings
mod cli
mod testing
