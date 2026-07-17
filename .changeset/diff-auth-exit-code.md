---
"@kurrent/gaffer": patch
---

Commands that fail because an environment needs an interactive sign-in now exit with code `4` (distinct from the generic `1`), so a caller can offer a sign-in rather than parsing the error text. This is what the VS Code **Diff against deployed** action keys off to surface its one-click sign-in.
