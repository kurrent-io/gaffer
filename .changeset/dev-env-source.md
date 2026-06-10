---
"@kurrent/gaffer": patch
---

`gaffer dev` resolves event sources more helpfully when `gaffer.toml` defines environments. The interactive source picker now offers each configured environment as a live option, not just the `default` one, so a single non-default environment is selected automatically and multiple are pickable. When no source resolves non-interactively, the error names the available environments and suggests `--env <name>` or `default = true`, rather than pointing you to configure an `[env.<name>]` you may already have.
