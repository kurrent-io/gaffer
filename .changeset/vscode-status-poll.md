---
"gaffer-vscode": patch
---

The per-projection status dots in `gaffer.toml` now refresh on a timer while the file is visible, so they track live runtime state (a projection stopping, faulting, or catching up) without re-opening or saving. Polling is scoped to the visible config editors and stops when none are visible. It also pauses while a file has unsaved edits, resuming on save. The language server keeps each poll cheap by reusing the cached drift verdict and reading only runtime state.
