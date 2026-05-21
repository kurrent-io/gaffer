---
title: Other editors
description: Attach Neovim, Helix, Emacs, or any DAP-aware editor to gaffer's debug server to step through projections.
---

Attach Neovim, Helix, Emacs, or any other DAP-aware editor to gaffer's debug server to step through projections from the editor of your choice (VS Code has its own [extension](./vs-code.md)).

## Start the DAP server

Start gaffer with `--debug` and a fixed port before attaching from your editor:

```sh
gaffer dev order-count --fixture happy --debug --debug-port 4711
```

The server listens on loopback only. With `--debug-port 0` (or omitted) the OS picks a free port and gaffer prints the chosen port on stderr.

Add `--start-paused-if-no-breakpoints` to pause at the first event even when no breakpoints are set, giving you a place to step from.

## Attach config shape

Most DAP clients accept an attach configuration shaped like:

```json
{
  "type": "gaffer",
  "request": "attach",
  "name": "Gaffer",
  "host": "127.0.0.1",
  "port": 4711
}
```

Field names vary by editor. The `type` value is an editor-side identifier you choose; what matters on the wire is `request: attach` and the host/port pair.

## Neovim

[nvim-dap](https://github.com/mfussenegger/nvim-dap) supports TCP-attached adapters directly. Register gaffer as an adapter and a configuration in your Neovim setup:

```lua
local dap = require('dap')

dap.adapters.gaffer = {
  type = 'server',
  host = '127.0.0.1',
  port = 4711,
}

dap.configurations.javascript = {
  {
    type = 'gaffer',
    request = 'attach',
    name = 'Attach to gaffer',
  },
}
```

Start `gaffer dev <projection> --debug --debug-port 4711` in a terminal, open the projection JS, then `:DapContinue` to attach. `:DapToggleBreakpoint` sets breakpoints; the rest of nvim-dap's commands (`:DapStepOver`, `:DapStepInto`, and so on) work as usual.

:::tip
The adapter's `port` and the `--debug-port` value must match. If you change one, change the other.
:::

## Helix

Helix's built-in DAP support is configured per-language in `languages.toml`. Add a debugger entry for JavaScript and a `templates` block for attach:

```toml
[[language]]
name = "javascript"

[language.debugger]
name = "gaffer"
transport = "tcp"
command = ""
args = []

[[language.debugger.templates]]
name = "attach"
request = "attach"
args = { host = "127.0.0.1", port = 4711 }
```

Start `gaffer dev <projection> --debug --debug-port 4711` in a terminal, then `:debug-start attach` from inside Helix.

:::note
Helix's DAP surface is still evolving and the attach-only path is lightly documented. Treat the snippet above as a starting point and cross-reference the [Helix debugger docs](https://docs.helix-editor.com/debugger.html) for the version you're running.
:::

## Emacs

[dap-mode](https://emacs-lsp.github.io/dap-mode/) registers debug configurations as named templates. Add gaffer as one:

```elisp
(require 'dap-mode)

(dap-register-debug-template
 "Gaffer Attach"
 (list :type "gaffer"
       :request "attach"
       :name "Gaffer Attach"
       :debugServer 4711))
```

`:debugServer` is dap-mode's TCP port key; the host defaults to localhost.

Start `gaffer dev <projection> --debug --debug-port 4711` in a terminal, then `M-x dap-debug` and pick **Gaffer Attach**. `dap-breakpoint-toggle` and the step commands handle the rest.

## Troubleshooting

- **`connection refused`**: gaffer is not running, or your editor is targeting a different port than the one gaffer bound. Check the port gaffer printed on stderr.
- **Session ends as soon as your editor attaches**: gaffer exited because the fixture completed, the subscription caught up, or the projection errored. Restart `gaffer dev` and re-attach. `--start-paused-if-no-breakpoints` gives you a pause point if you don't yet have breakpoints set.
