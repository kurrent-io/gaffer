---
"gaffer-vscode": patch
---

Each `[[projection]]` header in `gaffer.toml` now shows a row of small status dots, one per environment in file order. A filled green, orange, or red dot means in sync, needs attention, or faulted/invalid. A hollow ring, crossed ring, or faint dot means the environment needs sign-in, couldn't be read, or is still loading. Hovering the header lists each environment's verdict and runtime state. Read-only, read on open and save, like the environment summary.
