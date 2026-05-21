---
title: Gaffer
description: Develop, test, debug, and deploy KurrentDB projections with the gaffer toolkit.
---

Gaffer is a toolkit for working with [KurrentDB](https://www.kurrent.io) projections.

It includes a CLI to scaffold and run projections, a VS Code extension to debug them inline, a Node.js library to drive them from your test suite, and an MCP server so AI assistants can do all of the above.

## Get started

Install:

```sh
npm install -g @kurrent/gaffer
```

Then [write your first projection](./getting-started/first-projection.md).

## Sections

- **[Getting started](./getting-started/install.md)** - install, write your first projection, debug it.
- **[CLI](./cli/index.md)** - command reference and `gaffer.toml` schema.
- **[VS Code extension](./extension/vs-code.md)** - inline run/debug, state inspection, projection-API autocomplete.
- **[Other editors](./extension/other-editors.md)** - attach Neovim, Helix, or Emacs to gaffer's DAP server.
- **[Testing](./testing/nodejs.md)** - drive projections from vitest, jest, or mocha with `@kurrent/projections-testing`.
- **[MCP](./cli/mcp.md)** - connect Claude Code, Cursor, Continue, Claude Desktop, or any MCP-aware AI assistant.
