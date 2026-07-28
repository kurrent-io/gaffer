---
"gaffer-vscode": patch
---

Editor surfaces are now hidden when the installed `gaffer` CLI can't run them, rather than offered and failing on click. The extension updates from the marketplace while the CLI is installed separately, so a new extension routinely drives an older `gaffer`.

- The env-block **Deploy** lens is hidden unless the CLI can run the deploy spawns. The per-projection **Manage...** menu drops any row its CLI can't serve: **Deploy** and **History** are checked against the CLI's own command listing, **Diff against deployed** and the operate verbs against the methods the language server advertises. An environment left with no available rows collapses to a single **Unsupported** notice, matching the existing **Unavailable** and **Sign in** collapses. The **Manage...** lens itself is hidden when nothing at all is available.
- **Rollback** from the history viewer reports that it needs a newer CLI instead of failing the spawn, and does so before the confirmation prompt rather than after. Reading the timeline and rolling back are separate capabilities, so a CLI that can only read still gets a working read-only history.
- Deploy status is no longer polled against a language server that doesn't implement it. The poll previously wrote a `MethodNotFound` error into the **Gaffer (LSP)** output channel every five seconds for the whole session against an older CLI.
- An action that does reach a server without it now says the CLI needs updating, instead of surfacing the server's internal "method not implemented" wording.
