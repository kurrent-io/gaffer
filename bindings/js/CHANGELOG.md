# @kurrent/gaffer-runtime

## 0.1.0

### Minor Changes

- 5b85426: Low-level Node.js bindings for the gaffer projection runtime. Runs the same JavaScript engine KurrentDB uses for server-side projections, wrapping the NativeAOT shared library.
  - `ProjectionSession` - feed events, query state by partition, observe emits / logs / state changes, dispose.
  - `knownBugs()` - the runtime's list of known engine bugs by version, surfaced as actionable warnings in editor tooling.
  - Typed `ProjectionError` subclasses (handler errors, malformed events, compilation / execution timeouts, serialization failures) with structured fields.
  - Underpins [`@kurrent/projections-testing`](https://www.npmjs.com/package/@kurrent/projections-testing) and the [KurrentDB Projections VS Code extension](https://marketplace.visualstudio.com/items?itemName=kurrent-io.gaffer).
