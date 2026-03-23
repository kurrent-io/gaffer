default:
    @just --list

# Install all dependencies and tools
init: runtime::init bindings::init

# Build all projects
build: runtime::build

# Run all tests
test: runtime::test

# Check formatting across all projects
check: runtime::check

mod runtime
mod bindings
