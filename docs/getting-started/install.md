---
title: Install
description: Install the gaffer CLI for local projection development.
order: 1
---

Install the gaffer CLI for local projection development.

## Install the CLI

```sh
npm install -g @kurrent/gaffer
```

Requires Node.js 22 or later. Platform binaries are pulled in as optional dependencies.

Verify:

```sh
gaffer version
```

## Next

Write [your first projection](./first-projection.md).

## Optional add-ons

- **VS Code extension** - run/debug lenses on `gaffer.toml`, breakpoint debugging, and projection-API autocomplete. See [the extension](../extension/).
- **AI assistants** (Claude Code, Cursor, Continue, Copilot) - point them at `gaffer mcp` for scaffolding, debugging, and the projection API. See [MCP](../mcp/).
