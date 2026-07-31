---
"gaffer-vscode": patch
---

The extension now shows read-only deployment status in `gaffer.toml`, per environment and per projection.

**Above each `[env.<name>]` block**, a roll-up of how that environment's projections compare to what's deployed, a **Sign in** action when it needs authentication, or a **status unavailable** note when the read can't complete. A production target is flagged **PRODUCTION**.

**On each `[[projection]]` header**, a row of small status dots, one per environment in file order. A filled green, orange, or red dot means in sync, needs attention, or faulted/invalid. A hollow ring, crossed ring, or faint dot means the environment needs sign-in, couldn't be read, or is still loading. Hovering the header lists each environment's verdict and runtime state.

Status is read on open and save, refreshes on its own once you sign in, and the projection dots also refresh on a timer while the file is visible, so they track live runtime state - a projection stopping, faulting, or catching up - without re-opening or saving. Polling is scoped to the visible config editors and stops when none are visible; it pauses while a file has unsaved edits and resumes on save. The language server keeps each poll cheap by reusing the cached drift verdict and reading only runtime state.
