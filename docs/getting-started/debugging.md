---
title: Debugging projections
description: Set breakpoints, step through handlers, and inspect state with gaffer's DAP debug server.
order: 3
---

Gaffer's debug server speaks the Debug Adapter Protocol over TCP, so any DAP-aware editor can drive breakpoint debugging, step-through, and state inspection against a running projection.

## VS Code

The [KurrentDB Projections](https://marketplace.visualstudio.com/items?itemName=kurrent-io.gaffer) extension wires everything up: click **Debug** above the projection in `gaffer.toml`, set breakpoints in the JS, step through handlers as they run. The extension manages the gaffer subprocess and port for you.

## Other editors

For editors without a gaffer extension, start the DAP server yourself and attach your editor's debugger to it.

Start the server:

```sh
gaffer dev order-count --fixture happy --debug --debug-port 4711
```

The `--debug` flag starts a DAP server listening on `--debug-port`. With `--debug-port 0` (or omitted) the OS picks a free port and gaffer prints the chosen port on stderr.

`--start-paused-if-no-breakpoints` pauses at the first event when no breakpoints are set, giving you a place to step from when you haven't added breakpoints to the projection JS yet.

A generic attach config (exact field names depend on your editor):

```json
{
  "type": "gaffer",
  "request": "attach",
  "name": "Gaffer",
  "host": "localhost",
  "port": 4711
}
```

The workflow:

1. Run `gaffer dev <projection> --fixture <name> --debug --debug-port 4711` in a terminal.
2. Attach your editor's debugger to `localhost:4711`.
3. Set breakpoints in the projection JS.
4. Step through handlers as gaffer feeds events.

Per-editor setup varies. Consult your DAP client's docs for attach-config syntax. Common clients: [nvim-dap](https://github.com/mfussenegger/nvim-dap), Helix's built-in DAP support, and Emacs [dap-mode](https://emacs-lsp.github.io/dap-mode/).
