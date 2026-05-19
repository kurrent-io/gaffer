---
title: Install
description: Install the gaffer CLI for local projection development, and optionally the VS Code extension.
order: 1
---

Install the gaffer CLI for local projection development, and optionally the VS Code extension for debug-driven workflows.

## Install the CLI

```sh
npm install -g @kurrent/gaffer
```

Requires Node.js 22 or later. Platform binaries are pulled in as optional dependencies.

Verify:

```sh
gaffer version
```

## KurrentDB

Local development with fixture event files needs no running KurrentDB. Set it up only when you want to:

- Run a projection against a live event stream (`gaffer dev <projection>` without `--events` or `--fixture`).
- Deploy a projection to a running cluster (planned for a future release).

See [Installing KurrentDB](https://docs.kurrent.io/server/v26.1/quick-start/installation) when you're ready.

## VS Code

Install [KurrentDB Projections](https://marketplace.visualstudio.com/items?itemName=kurrent-io.gaffer) from the marketplace.

The extension wires up:

- **Debug**: CodeLens above each projection in `gaffer.toml`, breakpoint debugging, call-stack and state inspection.
- **Autocomplete**: projection builtins (`fromAll`, `when`, `emit`, `linkTo`, and the rest) typed correctly in your projection JS.
- **MCP integration**: an auto-registered MCP server so AI assistants (Copilot, Claude, Cursor) can scaffold, validate, and debug projections inside the editor.

## Other editors

Run `gaffer lsp` over stdio for Language Server Protocol integration. Configuration is editor-specific; consult your editor's LSP setup docs.

## Next

Write [your first projection](./first-projection.md).
