# Docs media harness

Regenerates the docs media: the VS Code extension screenshots under
`docs/public/vscode/` (this package) and, together with the tapes in
`../vhs/`, the CLI recordings under `assets/` and `docs/public/`. The
canonical run is the [Regenerate demo recordings](../../.github/workflows/regen-demos.yml)
workflow, which produces everything in one dispatch and opens a PR; this
README is the same flow for local iteration.

The screenshots drive a pinned, checksum-verified VS Code build headless
via playwright-core, photograph the extension against a seeded KurrentDB,
and encode light + dark WebP pairs. The CLI recordings are vhs tapes; the
deploy tapes run from a seeded workspace against the same database.

## Prerequisites

- The devcontainer (vhs, ttyd, ffmpeg, xvfb are preinstalled there).
- A login for the staging registry - the harness database is the
  KurrentDB staging nightly (see the pin exception in
  `docker-compose.yml`), so `db-up` fails without one:

  ```sh
  docker login docker.eventstore.com   # cloudsmith credentials
  ```

- A CLI build on PATH, with the runtime published first:

  ```sh
  just runtime publish && just cli build
  export PATH="$PWD/cli:$PATH"
  ```

- Inside a devcontainer, the compose port binds on the docker host, not
  the container's localhost - point the harness at it:

  ```sh
  export GAFFER_SCREENSHOTS_DB='kurrentdb://172.17.0.1:2113?tls=false'
  ```

  (Canonical shots use the default `localhost:2113`, which is what CI
  records; the connection string is visible in some frames.)

## The full run, in order

Recordings mutate server state as they go, so the order is load-bearing -
it mirrors `regen-demos.yml`:

```sh
just screenshots db-up            # start the seeded-state database
just screenshots prepare-tapes    # build + seed the tape workspace (.workspace)

cd demo                           # the standalone demo tapes record from demo/
for t in ../tools/vhs/*.tape; do VHS_NO_SANDBOX=1 vhs "$t"; done
cd -

cd tools/screenshots/.workspace   # deploy tapes run from the seeded workspace,
for t in ../../vhs/deploy/*.tape; do VHS_NO_SANDBOX=1 vhs "$t"; done   # in filename order
cd -

just screenshots db-reset         # the tapes deployed real state; capture seeds its own
just editors package              # the .vsix the screenshots install

xvfb-run -a --server-args='-screen 0 1280x800x24' just screenshots capture
just screenshots publish          # encode out/ into docs/public/vscode/
just screenshots db-down
```

`VHS_NO_SANDBOX=1` is required in containers (Chromium sandbox). With a
real display, drop the `xvfb-run` wrapper. To iterate on a single
surface, any phase runs alone against a fresh `db-reset` - just re-run
`prepare-tapes` before deploy tapes, since they consume its seed.

`capture` writes frames to `out/` and never touches the committed media;
`publish` is the only step that does, and it refuses partial or failed
capture sets. `clean` drops `out/`; `clean-cache` also drops the verified
VS Code download (needed after a pin bump in `src/vscode-pin.json`).
