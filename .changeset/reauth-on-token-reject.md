---
"@kurrent/gaffer": patch
---

gaffer now discards a stored OAuth token the identity provider has rejected (`invalid_grant`) and re-prompts for sign-in, instead of surfacing it as a generic connection failure. In the VS Code extension the "Sign in" action re-appears on the same run.
