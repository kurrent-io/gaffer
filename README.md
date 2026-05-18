<img src="docs/assets/banner-gaffer.svg" alt="Gaffer" width="100%">

# Gaffer

Develop, test, debug, and deploy [KurrentDB](https://kurrent.io) projections.

KurrentDB projections are server-side JavaScript that derive new streams and state from existing events. Gaffer uses the same JS engine as KurrentDB itself, so what you debug locally behaves like what runs in production.

## Install

CLI:

```sh
npm i -g @kurrent/gaffer
```

VS Code extension - install [KurrentDB Projections](https://marketplace.visualstudio.com/items?itemName=kurrent-io.gaffer) from the marketplace for run/debug from `gaffer.toml`, breakpoint debugging, and an auto-registered MCP server.

Test library:

```sh
npm i -D @kurrent/projections-testing
```

## Quick start

Run a projection from the CLI:

```sh
gaffer dev order-count --fixture happy
```

![gaffer dev replaying the order-count projection against the happy fixture](docs/assets/demo-dev.gif)

Or test it from your existing test suite:

```typescript
import { createProjection } from "@kurrent/projections-testing";

const projection = createProjection<{ count: number }>(`
  fromAll().when({
    $init: () => ({ count: 0 }),
    OrderPlaced: (s) => ({ count: s.count + 1 }),
  });
`);

for (const { state } of projection.run(events)) {
  // assert on state at each step
}
```

See the [demo project](demo/) for a complete example with fixtures, errors, partitioned state, and bi-state projections.

## Packages

| Component         | Package                                                                                      | License                          |
| ----------------- | -------------------------------------------------------------------------------------------- | -------------------------------- |
| CLI               | `@kurrent/gaffer`                                                                            | [Kurrent License v1](LICENSE.md) |
| VS Code extension | [`kurrent-io.gaffer`](https://marketplace.visualstudio.com/items?itemName=kurrent-io.gaffer) | [Kurrent License v1](LICENSE.md) |
| Test library      | `@kurrent/projections-testing`                                                               | [Apache 2.0](testing/js/LICENSE) |
| JS bindings       | `@kurrent/gaffer-runtime`                                                                    | [Kurrent License v1](LICENSE.md) |
| Go bindings       | `github.com/kurrent-io/gaffer/bindings/go`                                                   | [Kurrent License v1](LICENSE.md) |

Per-component `LICENSE` files live in each directory. See [LICENSE_CONTRIBUTIONS.md](LICENSE_CONTRIBUTIONS.md) for the license map and [NOTICE.md](NOTICE.md) for third-party attribution.

## Communities

- [Kurrent community](https://www.kurrent.io/community)
- [Discuss](https://discuss.kurrent.io/)
- [Discord (Kurrent)](https://discord.gg/Phn9pmCw3t)
- [Discord (ddd-cqrs-es)](https://discord.com/invite/sEZGSHNNbH)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and conventions. Bugs go to [Issues](https://github.com/kurrent-io/gaffer/issues); feature requests and questions to [Discussions](https://github.com/kurrent-io/gaffer/discussions).
