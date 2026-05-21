---
title: Getting started
description: Install gaffer, write your first projection, and step through it with the debugger.
order: 1
---

Gaffer runs KurrentDB projections locally so you can develop, test, and debug them without standing up a database. This section installs the CLI, walks through writing a first projection end-to-end, and shows how to attach a debugger.

## Install the CLI

```sh
npm install -g @kurrent/gaffer
```

Requires Node.js 22 or later. Platform binaries are pulled in as optional dependencies.

Verify:

```sh
gaffer version
```

If you install the [VS Code extension](../extension/) first and skip this step, the extension surfaces a status bar prompt that will run the install for you.

## Integrations

- **VS Code extension**: run/debug lenses on `gaffer.toml`, breakpoint debugging, and projection-API autocomplete. See [the extension](../extension/).
- **AI assistants** (Claude Code, Cursor, Continue, Copilot): point them at `gaffer mcp` for scaffolding, debugging, and the projection API. See [MCP](../mcp/).

## What's next

Write [your first projection](./first-projection.md).
