# Root justfile orchestrates cross-module dependencies and ordering.
# Sub-module justfiles are standalone - they just run their own commands.

default:
    @just --list

# Install all dependencies and tools
init: runtime::init bindings::init

# Build all projects
build: runtime::build runtime::publish

# Run all tests (publishes runtime first for FFI tests)
test: runtime::test runtime::publish bindings::go::test

# Check formatting and linting across all projects
check: runtime::check bindings::go::check

mod runtime
mod bindings
