# Releasing Gaffer

Gaffer uses [changesets](https://github.com/changesets/changesets) to manage versions and changelogs across the published packages.

## Adding a changeset

When you make a change that should ship to users, add a changeset alongside your code:

```sh
pnpm changeset
```

Pick the affected packages, the bump level (patch / minor / major), and write a one-line summary. The CLI writes a markdown file under `.changeset/`. Commit it in the same PR as the related code change.

Internal-tooling, docs-only, and test-only changes don't need a changeset.

## Lockstep groups

Two `fixed` groups in [`.changeset/config.json`](.changeset/config.json) keep native sub-packages in sync with their parent:

- **CLI cluster** - `@kurrent/gaffer` and its five `@kurrent/gaffer-<platform>-<arch>` native packages.
- **Runtime cluster** - `@kurrent/gaffer-runtime` and its five `@kurrent/gaffer-runtime-<platform>-<arch>` native packages.

Picking either parent in the changeset prompt bumps the whole cluster to the same version. The two clusters version independently of each other.

`@kurrent/projections-testing` and the `gaffer` VS Code extension version independently as well. When `@kurrent/gaffer-runtime` patches, `@kurrent/projections-testing` also receives a patch automatically (`updateInternalDependencies: "patch"`) so its `dependencies` pin tracks runtime patches.

## How a release happens

1. Your PR merges to `main` with one or more changesets in `.changeset/`.
2. The `Release` workflow runs `changesets/action`. With pending changesets present, the action opens (or updates) a **Version Packages** PR aggregating every pending changeset, with the proposed version bumps and per-package changelog entries.
3. Reviewing and merging the Version Packages PR runs `changeset version`, which:
   - Bumps `version` fields in the affected `package.json` files
   - Generates / updates per-package `CHANGELOG.md`
   - Deletes the consumed changeset markdown files
4. The merge commit lands with no remaining changesets. On the next `main` run, `changesets/action` finds nothing to consume and runs its `publish:` command, which dispatches the [`Release publish`](.github/workflows/release-publish.yml) workflow (a single-runner `publish:` slot can't host the cross-OS native matrix). That workflow does the actual publishing:
   - **npm packages** - builds the per-platform native binary matrices, then publishes the CLI cluster (`@kurrent/gaffer` + its native packages), the runtime cluster (`@kurrent/gaffer-runtime` + its native packages), and `@kurrent/projections-testing`, all with provenance via npm trusted publishing.
   - **VS Code extension** - builds the `.vsix` and publishes to Open VSX and the Visual Studio Marketplace. `skipDuplicate` makes this a no-op unless `editors/vscode/package.json`'s version changed, so the extension ships only when its changeset bumped the version. It stays `private: true` so the npm publishes skip it.
   - **Production deploys** - the telemetry worker and docs site redeploy to production on the same run.

   `release-publish.yml` also takes a manual `workflow_dispatch` with `dry_run: true` for a publish smoke-test.

## Telemetry worker deploys

The telemetry worker isn't a published package, but its deploys ride the same workflow:

- **Staging** - redeployed on every push to `main` that touches `telemetry/worker/`, `telemetry/generated/`, `telemetry/schemas/`, or `release.yml`.
- **Production** - redeployed on every publish run (Version Packages PR merge), regardless of whether worker files changed. Coarse but idempotent.
- **Manual hotfix** - run `just telemetry worker deploy-production` locally with `CLOUDFLARE_API_TOKEN` set.

PR validation happens through the worker's own `test` script (`pnpm --filter @kurrent/gaffer-telemetry-worker test`), which now ends with `wrangler deploy --dry-run`. Cloudflare creds aren't needed for the dry-run, so it runs in normal PR CI.

## Cadence

No fixed release cadence in v0. Cut releases when there's something worth shipping. Smaller, more frequent releases are preferable to long-lived version PRs.
