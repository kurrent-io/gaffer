# Root justfile orchestrates cross-module dependencies and ordering.
# Sub-module justfiles are standalone - they just run their own commands.

default:
    @just --list

# Root-level tool setup: corepack first, then pnpm. Both must precede the
# per-module non-pnpm init step that follows.
[private]
_init-root:
    corepack enable
    corepack install

# Workspace-wide pnpm install. Runs once at the repo root - pnpm 10.x's
# workspace handles every package in pnpm-workspace.yaml in a single pass.
# Splitting this across modules used to race on `mkdir` under .pnpm/
# because pnpm doesn't lock its content-addressed store across concurrent
# processes; one install eliminates the contention.
[private]
_init-pnpm:
    pnpm install

# Install per-module dev tools that don't share the pnpm store: dotnet
# restore (runtime), golangci-lint (Go bindings), cue (telemetry). Safe
# to run in parallel because these touch independent caches.
[private]
[parallel]
_init-modules: runtime::init bindings::init telemetry::init

# Install all dependencies and tools, then generate telemetry types so IDEs
# resolve them on first open.
init: _init-root _init-pnpm _init-modules telemetry::build

# Ensure runtime .so is built (sequential: build then publish)
[private]
_runtime: runtime::build runtime::publish

# Generate telemetry types from the CUE source. Must run before any module
# build that consumes the generated Go / TS types.
[private]
_telemetry: telemetry::build

# Build all projects that can build in parallel (after runtime + telemetry)
[private]
[parallel]
_build: cli::build bindings::build testing::build editors::build types::build docs::build

# Build everything
build: _runtime _telemetry _build

# Run all test suites in parallel (after runtime is published)
[private]
[parallel]
_test: runtime::test bindings::test cli::test testing::test editors::test types::test

# Run all tests
test: _runtime _telemetry _test

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
test-integration: _runtime _telemetry _test-integration

# Format all code and apply lint fixes
[parallel]
fix: runtime::fix bindings::fix cli::fix testing::fix editors::fix types::fix telemetry::fix docs::fix

# Check formatting and linting across all projects
[parallel]
check: runtime::check bindings::check cli::check testing::check editors::check types::check telemetry::check docs::check

# Remove build artifacts across all projects
[parallel]
clean: runtime::clean bindings::clean cli::clean testing::clean editors::clean types::clean telemetry::clean docs::clean

mod runtime
mod bindings
mod cli
mod testing
mod editors 'editors/vscode'
mod types
mod telemetry
mod docs
