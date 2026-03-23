default:
    @just --list

# Build all projects
build: runtime::build

# Run all tests
test: runtime::test

# Check formatting across all projects
check: runtime::check

mod runtime
