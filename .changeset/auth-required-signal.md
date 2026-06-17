---
"@kurrent/gaffer": patch
---

`gaffer dev --json` now emits an `auth_required` message when a live run can't authenticate without an interactive sign-in (no stored token, or a keyring that can't be unlocked non-interactively), instead of failing with a generic connection error. The VS Code extension uses this to offer a "Sign in" action that runs `gaffer auth` for you.
