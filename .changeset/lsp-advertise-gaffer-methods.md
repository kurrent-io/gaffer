---
"@kurrent/gaffer": patch
---

The language server now lists the `gaffer/*` requests it serves in its initialize result, under `capabilities.experimental.gaffer.methods`. LSP has standard capability fields for standard features but no slot for a server's own requests. An editor extension driving an older `gaffer` previously had no way to discover support except to send a request and read `MethodNotFound` off the failure, by which point the user had already clicked something. Editors can now hide an action their CLI can't run.
