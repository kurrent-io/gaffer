# @kurrent/projections-testing

## 0.1.2

### Patch Changes

- 3b5392c: Documentation links in the README now point at `gaffer.kurrent.io` rather than the `docs.kurrent.io/gaffer/` placeholder.
- Updated dependencies [3b5392c]
  - @kurrent/gaffer-runtime@0.1.2

## 0.1.1

### Patch Changes

- 2675301: Republish to track `@kurrent/gaffer-runtime@^0.1.1`. The runtime's native binary was missing in 0.1.0, so any test using this library at 0.1.0 failed to load the projection engine.
- Updated dependencies [2675301]
  - @kurrent/gaffer-runtime@0.1.1

## 0.1.0

### Minor Changes

- 5b85426: Test KurrentDB projections locally with any test runner (vitest, jest, mocha). Wraps `@kurrent/gaffer-runtime` to execute projections against test events with the same behaviour as a real KurrentDB instance.
  - `createProjection<TState>(source, options?)` - prepare a projection for repeated runs. Compiles lazily on first `validate` / `run` / `test`.
  - `projection.validate()` - compile and return the projection's source definition.
  - `projection.run(events)` - replay events from an array, async iterable, or live `KurrentDBClient` subscription. Yields a `StepResult` per event.
  - `projection.test()` - interactive session: feed events one at a time, query state by partition, inspect emitted events and logs.
  - `systemEvents` helpers for constructing KurrentDB system events (e.g. `streamDeleted`).
  - Typed `ProjectionError` subclasses propagated from the runtime with structured fields and formatted messages.

### Patch Changes

- Updated dependencies [5b85426]
  - @kurrent/gaffer-runtime@0.1.0
