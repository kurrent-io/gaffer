---
"@kurrent/gaffer": patch
---

The language server now lists the `gaffer/*` requests it serves in its initialize result, under `capabilities.experimental.gaffer.methods`. LSP has standard capability fields for standard features but no slot for a server's own requests, so an editor extension driving an older `gaffer` previously had no way to discover support other than sending a request and reading `MethodNotFound` off the failure - after the user had already clicked something. Editors can now hide an action their CLI can't run instead of offering one that errors.
