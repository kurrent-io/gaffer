---
"@kurrent/gaffer": minor
---

`gaffer scaffold` now takes an explicit file path instead of a bare projection name. The bare-name form is gone; users must pass a path that ends in a supported extension (`.js` today).

```
# before
gaffer scaffold counter

# after
gaffer scaffold projections/counter.js
```

The toml key (the projection's name in `gaffer.toml`) defaults to the file's basename without extension. Override with `--name` when the file name and toml key should differ:

```
gaffer scaffold projections/totals.js --name order-totals
```

Same shape on the MCP `scaffold` tool: `path` is now a required field, `name` is optional and defaults to the basename. Path is cwd-relative on the CLI and project-root-relative on MCP; both surfaces normalise backslashes to forward slashes, validate that the path stays inside the project root (including through symlinks), and reject paths without a supported extension or with no filename stem (`.js`, `foo/.js`).
