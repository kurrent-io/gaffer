# @kurrent/gaffer-runtime

Low-level Node.js bindings for the gaffer projection runtime. Wraps the NativeAOT shared library via [koffi](https://koffi.dev/).

Most users want [`@kurrent/projections-testing`](https://www.npmjs.com/package/@kurrent/projections-testing), which builds on this package and provides a test-runner-friendly API. Reach for this package directly only when you need the raw session surface (custom test ergonomics, bespoke event sourcing tools, language tooling).

## Install

```sh
npm install --save-dev @kurrent/gaffer-runtime
```

Requires Node.js 22 or later. Platform binaries are pulled in as optional dependencies; no separate runtime install is needed.

## Quick start

```typescript
import { ProjectionSession } from "@kurrent/gaffer-runtime";

const session = new ProjectionSession(
	`fromAll().when({
     $init: () => ({ count: 0 }),
     OrderPlaced: (s) => ({ count: s.count + 1 }),
   })`,
	{ engineVersion: 2 },
);

session.feed({
	eventType: "OrderPlaced",
	streamId: "order-1",
	sequenceNumber: 0,
	isJson: true,
	eventId: "00000000-0000-0000-0000-000000000001",
	created: "2026-01-01T00:00:00Z",
	data: "{}",
});

console.log(session.getState()); // {"count":1}

session.dispose();
```

## API

The public surface is exported from the package root:

- **`ProjectionSession`** - the session class. Construct with source + options, then `feed()`, `getState()` / `getSharedState()` / `getResult()`, `setState()`, `getSources()`, `getPartitionKey()`, `onEmit()` / `onLog()` / `onStateChanged()`, `dispose()`.
- **`knownQuirks()`** - returns the runtime's list of known engine quirks by version. Useful for surfacing actionable warnings in editor tooling.
- **Error classes** - `ProjectionError` base plus `InvalidProjectionError`, `CompilationTimeoutError`, `InvalidArgumentError`, `ProjectionHandlerError`, `ExecutionTimeoutError`, `MalformedEventError`, `StateSerializationError`, `ProjectionTransformError`. All carry structured fields (`code`, `description`, event context where applicable).
- **Types** - `ProjectionEvent`, `EmittedEvent`, `FeedResult`, `ProjectionInfo`, `SessionOptions`, `Diagnostic`, `SourceRange`, `SourcePosition`, `DiagnosticSeverity`.

The TypeScript declarations are the source of truth for the surface; see [`src/index.ts`](src/index.ts) for the full export list.

## Related packages

| Package                                                                                      | What it is                                                               |
| -------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
| [`@kurrent/projections-testing`](https://www.npmjs.com/package/@kurrent/projections-testing) | Higher-level test API built on this package. Vitest/Jest/Mocha-friendly. |
| [`@kurrent/gaffer`](https://www.npmjs.com/package/@kurrent/gaffer)                           | CLI to scaffold, run, debug, and deploy projections                      |
| [KurrentDB Projections for VS Code](https://gaffer.kurrent.io/extension/vs-code/)            | Editor integration with debugger, codelens, and MCP server               |

## Documentation

Full documentation at <https://gaffer.kurrent.io/>.

Bugs go to [GitHub Issues](https://github.com/kurrent-io/gaffer/issues). Questions and feature requests to [Discussions](https://github.com/kurrent-io/gaffer/discussions).

## License

[Kurrent License v1](LICENSE)
