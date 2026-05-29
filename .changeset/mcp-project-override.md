---
"@kurrent/gaffer": patch
---

`gaffer mcp` can now be pointed at a project explicitly, instead of only searching upward from the working directory. This matters when the server is registered globally and launched from an arbitrary directory.

- A `--project <dir>` flag and a `GAFFER_PROJECT` environment variable, each accepting a project root or any directory inside it (gaffer walks up to find the `gaffer.toml`).
- Precedence: `--project` over `GAFFER_PROJECT` over the working-directory search.
- When the override points somewhere without a `gaffer.toml`, the server still starts; the project tools' error names the path you gave so the misconfiguration is obvious.
