---
"@kurrent/gaffer": patch
---

The language server now serves per-projection deploy status. Hovering a `[[projection]]` header lists each environment's drift verdict and runtime state, one per line with a colored status dot. Alongside it, the server emits each environment's health on the header (in file order) for editors to render as a row of inline badges. Both read from the same status cache as the environment roll-up and are gated on the same editor opt-in.
