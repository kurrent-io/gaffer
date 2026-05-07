# Root justfile orchestrates cross-module dependencies and ordering.
# Sub-module justfiles are standalone - they just run their own commands.

default:
    @just --list

# Root-level tool setup
[private]
[parallel]
_init-root:
    corepack enable
    corepack install

# Install all module dependencies in parallel
[private]
[parallel]
_init-modules: runtime::init bindings::init testing::init editors::init types::init

# Install all dependencies and tools
init: _init-root _init-modules

# Ensure runtime .so is built (sequential: build then publish)
[private]
_runtime: runtime::build runtime::publish

# Build all projects that can build in parallel (after runtime)
[private]
[parallel]
_build: cli::build bindings::build testing::build editors::build types::build

# Build everything
build: _runtime _build

# Run all test suites in parallel (after runtime is published)
[private]
[parallel]
_test: runtime::test bindings::test cli::test testing::test editors::test types::test

# Run all tests
test: _runtime _test

# Start KurrentDB for integration tests
db-up:
    docker compose -f tools/kurrentdb/docker-compose.yml up -d --wait

# Stop KurrentDB
db-down:
    docker compose -f tools/kurrentdb/docker-compose.yml down

# Run integration tests (requires KurrentDB)
[private]
[parallel]
_test-integration: cli::test-integration testing::test-integration

# Run integration tests (requires KurrentDB)
test-integration: _runtime _test-integration

# Format all code and apply lint fixes
[parallel]
fix: runtime::fix bindings::fix cli::fix testing::fix editors::fix types::fix

# Check formatting and linting across all projects
[parallel]
check: runtime::check bindings::check cli::check testing::check editors::check types::check

# Remove build artifacts across all projects
[parallel]
clean: runtime::clean bindings::clean cli::clean testing::clean editors::clean types::clean

mod runtime
mod bindings
mod cli
mod testing
mod editors 'editors/vscode'
mod types
