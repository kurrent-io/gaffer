# Changesets

This directory holds [changesets](https://github.com/changesets/changesets) — short notes describing changes that should appear in a future release.

## Adding a changeset

When you make a change that should ship to users, add a changeset:

```sh
pnpm changeset
```

Pick the affected packages, the bump level (patch / minor / major), and write a one-line summary. The CLI writes a markdown file here that you commit alongside your code change.

## Lockstep behaviour

Two `fixed` groups in [`config.json`](config.json) keep native sub-packages in sync with their parent:

- **CLI cluster** — `@kurrent/gaffer` and its five `@kurrent/gaffer-<platform>-<arch>` native packages
- **Runtime cluster** — `@kurrent/gaffer-runtime` and its five `@kurrent/gaffer-runtime-<platform>-<arch>` native packages

Picking either parent in the changeset prompt bumps the whole cluster to the same version. The two clusters version independently of each other, as do `@kurrent/projections-testing` and `gaffer` (the VS Code extension).

## What happens next

1. Your PR merges to `main` with the changeset.
2. The `changesets/action` workflow opens (or updates) a "Version Packages" PR aggregating every pending changeset, with the proposed version bumps and changelog entries.
3. Reviewing and merging that PR runs `changeset version`, which bumps `package.json` versions and regenerates the per-package `CHANGELOG.md`.
4. Each package's publish workflow takes over from there.

See [`RELEASING.md`](../RELEASING.md) at the repo root for the full release process.
