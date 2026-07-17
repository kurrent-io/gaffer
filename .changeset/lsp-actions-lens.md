---
"@kurrent/gaffer": patch
---

The language server now emits a per-projection **Manage...** CodeLens above each `[[projection]]` header in `gaffer.toml`, carrying the projection and its configured environments. It's the entry point to a client-rendered action menu (diffing local against deployed today; more to follow). Editors opt in via the same `statusLens` initialization option as the deployment-status lenses, so it's a no-op for clients that don't render it.
