---
title: Debugging projections
description: Set breakpoints, step through handlers, and inspect state with gaffer's DAP debug server.
---

Gaffer's debug server speaks the Debug Adapter Protocol over TCP, so any DAP-aware editor can drive breakpoint debugging, step-through, and state inspection against a running projection.

## VS Code

The KurrentDB Projections extension wires everything up: click **Debug** above the projection in `gaffer.toml`, set breakpoints in the JS, step through handlers as they run. The extension manages the gaffer subprocess and port for you. Install it from the [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=kurrent-io.gaffer-vscode) or [Open VSX](https://open-vsx.org/extension/kurrent-io/gaffer-vscode).

## Other editors

For editors without a gaffer extension, start the DAP server with `gaffer dev --debug --debug-port <port>` and attach over TCP. See [Other editors](../extension/other-editors.md) for attach configs for Neovim, Helix, and Emacs.
