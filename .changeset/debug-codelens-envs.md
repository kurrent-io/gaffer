---
"gaffer-vscode": patch
---

Debug CodeLenses are now environment-aware. The projection-level **Debug** lens runs live against the default environment, or the sole configured one, and is hidden when there's no unambiguous target so it no longer faults. **Debug from fixture...** becomes **Debug from...**: a single picker offering the projection's fixtures and every configured environment, so a non-default environment is reachable without editing `gaffer.toml`.
