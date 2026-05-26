# @kurrent/gaffer-runtime

## 0.1.2

### Patch Changes

- 3b5392c: Documentation links in the README now point at `gaffer.kurrent.io` rather than the `docs.kurrent.io/gaffer/` placeholder.

## 0.1.1

### Patch Changes

- 2675301: Republish the per-platform native packages with their compiled `gaffer.so` / `.dylib` / `.dll`. 0.1.0 shipped those packages empty due to a CI workflow bug (`upload-artifact@v4` strips directory paths for single-file uploads, so the download step at publish time saw colliding bare-named files at the workspace root instead of files in their per-platform package dirs). Installing 0.1.0 left koffi unable to load the runtime. Reinstall `>=0.1.1` to pick up the fix.

## 0.1.0

### Minor Changes

- 5b85426: Low-level Node.js bindings for the gaffer projection runtime. Runs the same JavaScript engine KurrentDB uses for server-side projections, wrapping the NativeAOT shared library.
  - `ProjectionSession` - feed events, query state by partition, observe emits / logs / state changes, dispose.
  - `knownBugs()` - the runtime's list of known engine bugs by version, surfaced as actionable warnings in editor tooling.
  - Typed `ProjectionError` subclasses (handler errors, malformed events, compilation / execution timeouts, serialization failures) with structured fields.
  - Underpins [`@kurrent/projections-testing`](https://www.npmjs.com/package/@kurrent/projections-testing) and the [KurrentDB Projections VS Code extension](https://marketplace.visualstudio.com/items?itemName=kurrent-io.gaffer).
