# @kurrent/gaffer

## 0.5.0

### Minor Changes

- bbfd56c: **Breaking:** `gaffer.toml` gains a `[database_config]` section for node-level engine settings, and the top-level `compilation_timeout` / `execution_timeout` keys move into it. A file that still sets them at the top level now fails to load, with a message pointing at the new section.

  `[database_config]` declares the engine configuration expected on a deployment target:

  - `max_state_size` (newly exposed) caps a projection's serialized state in bytes, defaulting to the server's 16 MiB. It is enforced on local runs, so a projection that would exceed the cap faults locally, catching state bloat before deploy.
  - `compilation_timeout` and `execution_timeout` are declaration only: gaffer records them for deploy-time configuration checks but does not apply them to local runs, since a wall-clock budget measured on a dev machine isn't comparable to the server's. To bound how long a local projection may run before gaffer treats it as hung, set the `GAFFER_TIMEOUT_MS` environment variable (default 5000ms). A per-`[[projection]]` `execution_timeout` is likewise declaration only and no longer affects local runs.

  **`gaffer deploy` and `gaffer status` check the target against the declaration** and warn when the node's live engine settings diverge: one line per differing knob, read from the node's options endpoint. Fixtures and local runs assumed the declared values, so a server enforcing a different `max_state_size` or timeout is visible before it bites. A non-positive `max_state_size` declares the engine default rather than a value.

  The node-options read authenticates exactly like the connection itself: connection-string userinfo, credentials from `.env` / `.env.<env>`, an OAuth bearer token (client-credentials or the login stored by `gaffer auth`, never prompting), or the environment's user certificate presented in the TLS handshake, honouring the connection's `tlsCaFile` and `tlsVerifyCert` settings.

  The check is advisory, but a failure is visible rather than silent. When the node's options can't be read (no HTTP surface, an auth refusal), `status` and `deploy` warn that the check couldn't run instead of reporting a false "no drift", the JSON envelopes carry `configDriftError`, and the MCP deploy confirmation notes the unchecked config. `gaffer status --json` reports a divergence as a `configDrift` array of `{"knob", "server", "local"}`. `gaffer deploy --json` keeps its warning on stderr, so the stdout payload stays clean while CI logs still show it.

- 331f061: **Breaking:** `gaffer.toml` now rejects absolute `entry` and `fixtures.<name>` paths at load time. Previously an absolute path (e.g. `entry = "/etc/passwd"`, or a Windows drive-letter form like `C:\...`) slipped past validation while the scaffold write path already rejected it. Both surfaces now enforce the same rule: paths must be relative to the project root and must not escape it.

### Patch Changes

- bbfd56c: `gaffer deploy` creates or updates projections on an environment from `gaffer.toml`: it creates the ones not yet on the server, updates the ones whose definition changed, and skips the ones already in sync (matched by content hash). With no argument it deploys every projection in `gaffer.toml`; name one to deploy just it. The emit flag is always sent explicitly, so an update never clears it.

  **Plan, then validate, then apply.** Deploy builds the whole plan against the server before touching it, then compiles the projections it would create or update. If any won't run (one that fails to compile, or that compiles but carries errors which would fault on the server), it refuses the run before writing anything, so a bad projection can't leave the earlier ones already applied. `--no-validate` skips the check, deploying the valid projections and refusing the invalid ones individually instead of aborting the whole run.

  **The plan preview** lists each projection with its verdict (`create`, `update`, `rebuild`, `refused`, `invalid`, or `failed`) and a dimmed detail column carrying the refusal reason or the failure error in full. In-sync projections stay a count only, so unchanged ones don't drown the signal. It names the target's reported cluster name, flags an update over a currently-faulted projection (the update won't clear the fault), and cautions when a projection's deployed definition was changed outside gaffer since its last deploy, naming the tool that changed it. Gaffer is the canonical source of truth, so it still deploys: the drift is surfaced, not refused.

  **Confirmation** follows the plan. `--yes` skips the prompt; without a terminal (or with `--json`) deploy won't apply unconfirmed, so pass `--yes` in scripts. A production target (a server that declares itself production, or an env with `production = true`) gets a louder confirmation and refuses `--no-validate`.

  **A changed query is a logic change:** the new code may read already-processed events differently, so the accumulated state could be wrong. By default deploy keeps the checkpoint, applies the update, and flags the change. Pass `--reset-on-logic-change` to rebuild instead: each logic-changed projection is stopped, updated, reset to the beginning, and restarted so it reprocesses from zero. An emitting projection re-emits on a rebuild and may duplicate into its target streams, so the plan warns and points at `gaffer recreate --delete-emitted` for a clean-emit rebuild. A change to engine version or track-emitted-streams can't be applied in place at all; deploy refuses it and points at `gaffer recreate`.

  **Machine-readable output.** `--json` emits the per-projection results as an array. `--dry-run` shows the plan and applies nothing; `--dry-run --json` emits a structured envelope carrying a top-level `verdict` of `in-sync`, `deployable`, or `blocked` (what a real deploy would do), the `changes` count, the resolved `env` and `target`, whether the target is `production`, any `[database_config]` divergence (`configDrift` / `configDriftError`), and the per-projection `plan` array. Each item reports its would-be `outcome` plus the flags a structured consumer needs:

  - **`recreate`**: the `refused` outcome is an engine-version or track-emitted-streams change needing a recreate, not an invalid definition.
  - **`faulted`**: an update over a currently-faulted projection.
  - **`emittingReset`**: a rebuild that re-emits.
  - **`logicChange`**: a continued logic change, so CI can alert on it.
  - **`externalChange`** / **`externalChangeTool`**: the deployed definition was changed out of band, and the tool that changed it.

  `--json --stream` streams the apply as newline-delimited JSON instead of buffering a single array until the run finishes. Each line is a `type`-tagged event: a `deploy_start` as each projection's RPC begins, a `deploy_result` as it settles (the same per-item shape `--json` emits), and a terminal `deploy_summary` counting the outcomes, so a consumer can render progress live. `--stream` is for the apply: it requires `--json` and can't be combined with `--dry-run`. stdout stays strictly NDJSON. A pre-apply invalid-plan refusal streams the invalid projections then a `deploy_summary` reporting nothing applied, and a run with nothing to deploy emits a single zeroed `deploy_summary`, so a streaming consumer always ends on one. A broken output stream never aborts an in-flight deploy: emitting goes quiet after the first write error and the apply runs to completion.

  **Exit codes** are stable for scripts: `0` succeeded or nothing to do, `1` an error, `2` changes are pending (`--dry-run` only), `3` refused by a guardrail: confirmation was needed but there was no terminal or `--yes`, or `--no-validate` was used against production. Under `--dry-run` the code follows the verdict: `0` in-sync, `2` deployable, `1` blocked. The guardrail exit code `3` also applies to `recreate` and the operate verbs when they can't confirm non-interactively.

  Degrades silently against a KurrentDB without the deploy-metadata field: the external-change detection is skipped and deploy behaves as it otherwise would.

- bbfd56c: Gaffer now records tool metadata on every projection it writes, so a projection carries who changed it and from where: the tool (`Gaffer`) and version, the operation, the source revision, and the acting identity. It follows a shared convention that other KurrentDB tools can write and display, and it's what `gaffer status`, `gaffer diff`, and `gaffer history` read to attribute a change.

  Every mutating command stamps its operation: `gaffer deploy` records `deploy`, `gaffer recreate` records `recreate`, and `gaffer rollback` records `rollback`. A recreate is therefore attributed to gaffer rather than appearing as anonymous lifecycle steps.

  - **`revision`** defaults to the project's git commit (suffixed `+changes` when the working tree is dirty); set `GAFFER_REVISION` in CI to record the canonical commit.
  - **`actor`** defaults to the identity gaffer connects as (the basic-auth user or OAuth client), omitted for an anonymous connection; set `GAFFER_ACTOR` in CI to record the pipeline identity.

  The metadata rides on the projection's definition event and is best-effort: against a KurrentDB that predates the feature it is silently ignored and the command behaves exactly as before.

- bbfd56c: `gaffer status` shows the runtime state of projections on an environment and how they compare to local config: running, stopped, aborted or faulted, progress, and whether each is in sync, drifted, not deployed, untracked, or invalid. With no argument it lists every local and deployed projection as a table; name a projection for its detail. Pass `--json` for machine-readable output.

  **Ownership and drift are attributed** from the tool metadata gaffer stamps on deploy, so the table says more than `untracked` or `drifted`:

  - A projection on the server but not in local config reads as `orphan` (gaffer deployed it, now gone from `gaffer.toml`, so it's a deletion candidate) or plain `untracked`, with the deploying tool named when its metadata is present. `--json` reports this as `owner`, including `foreign` for a projection another tool manages.
  - An in-config projection that differs from what's deployed reads as `local ahead` (you've edited local since your deploy) or `changed externally` (a tool or a direct write changed the server since). `--json` splits the latter into `changed-by-tool` and `changed-server`.

  The table gains **LAST DEPLOY** and **DEPLOYED VIA** columns, and naming a projection shows the deploy provenance behind it: when, the tool and version, the deployer, and the source revision. The last-deploy date comes from the event itself, so it shows even for a projection with no tool metadata.

  **An aborted projection** reports its own `aborted` runtime state, distinct from a clean `stopped`. An aborted projection was stopped without a final checkpoint, so resuming it reprocesses from the last checkpoint written, re-emitting for an emitting projection. The state surfaces everywhere `runtime.state` appears: the status table (with a warning tint), the detail block, `--json`, and the `deploy_status` MCP tool. The signal is transient. KurrentDB reports it only while it holds the projection in memory, so it reverts to `stopped` after a server restart, and the absence of `aborted` is not proof of a clean pause.

  **A projection that fails to compile no longer aborts the command.** A compile error is a per-projection condition: `gaffer status` shows the broken projection as `invalid` and still renders the rest of the table with their real runtime state and drift. It exits 0, and the compile error is shown so you know what to fix.

  `gaffer status --json` emits an object: a `projections` array plus a `configDrift` array when the target diverges from `[database_config]`. Each projection carries `owner`, `attribution`, a top-level `lastDeployed` timestamp, `lastWrite` (the tool, its version, the source revision, and the actor), and the deployed definition's content `hash`. The report also names where it landed (the resolved `env`, `target` server, and `production` tier), so it's self-describing without a second call.

  Against a KurrentDB without the deploy-metadata field, the attribution degrades to plain in-sync / drifted / untracked.

- bbfd56c: `gaffer diff <projection>` compares two versions of a projection and reports how they differ. By default it compares the local definition against what's deployed on KurrentDB, reporting its state: in sync, drifted, not deployed, untracked, or invalid. `--left` and `--right` compare any two versions instead. Each is `local`, `deployed`, or a content-hash prefix from `gaffer history` (resolving a hash costs a history read). A version-to-version diff is a pure source diff with no state verdict.

  **The source diff renders in-process** rather than shelling out to an external viewer. Every line of both sides is shown with the changes marked in place: dual line-number gutters, +/- colouring, and the span that changed within a line highlighted. The diff is computed on the same canonical form as the drift verdict, so it always matches the `+N -M` stat that `gaffer diff` and `gaffer status` report. It works without git installed, when piped, and in CI. Set `GAFFER_EXTERNAL_DIFF` to open an external viewer instead (e.g. `git diff`, `delta`, `difft`); it is no longer the default path.

  **The drift verdict is attributed** from the tool metadata gaffer stamps on deploy, matching `gaffer status`: a projection on the server but not in local config reads as `orphan` or `untracked` (naming the deploying tool where known), and one that differs from what's deployed reads as `local ahead` or `changed externally`. `gaffer diff` also shows the deploy provenance behind the projection: when it was deployed, the tool and version, the deployer, and the source revision.

  **A projection that fails to compile no longer aborts the command.** `gaffer diff` still shows the source diff, engine version, and track-emitted-streams, marking `emit` unknown because deriving it needs a successful compile. It exits 0, and the compile error is shown so you know what to fix.

  Pass `--json` for machine-readable output. It carries the two sides as `left` and `right`, each with its `ref`, content `hash`, and canonical `source`. A structured `lines` array gives each row a kind (`equal`, `removed`, or `added`), per-side line numbers, and the changed intraline span. On the default deployed-vs-local diff it adds a `verdict` with the drift state, plus the `owner` and `attribution` fields `gaffer status --json` reports.

- bbfd56c: `gaffer history <projection>` shows a deployed projection's history: every operation on it, newest first, with who made it and how.

  - On a terminal it opens an interactive timeline: a scrolling list on the left, the selected entry's full detail on the right, and a footer naming the projection and target. Navigate with `↑`/`↓` (or `j`/`k`), `g`/`G`, `PgUp`/`PgDn`; `q` or `Esc` quits. Older entries page in as you scroll.
  - Each entry is one write to the projection. One carrying gaffer metadata shows its operation (deploy, recreate, rollback, reset), the actor, and the source revision. One without is attributed by what changed: `updated` when the definition moved, `updated-by` (with the tool named) for another tool's write, `enabled`/`disabled` for a lifecycle change, `reconfigured` when a checkpoint or performance setting moved, `rewritten` for an identical redeploy, or `created`/`deleted`.
  - A content hash identifies each deployed definition, so a reverted definition is recognisable at a glance: the timeline draws a revert as a branch off the live line, linking the restored definition back to the earlier one it matched (nested reverts included). A recreate shows as a single entry: the disable and delete writes it performs are folded into the `recreate` row, and the detail panel notes the projection was reprocessed from zero.
  - Piped or with `--json` it prints the latest entries instead (`--limit`, default 100, or `--all`). Each `--json` entry carries the full content hash, its classification and flags, the tool metadata, and any configuration knobs that moved. A metadata-less `updated` entry also carries a `changeSummary` naming what moved (e.g. `query changed`). It's the same summary the terminal timeline prints, so a consumer doesn't have to fetch and diff the two versions to describe the change. A recreate keeps every underlying write as its own entry, with the create's `kind` set to `recreate`.

  **`d` opens a diff** of the selected entry against the version before it, in an overlay on the timeline. It answers "what changed at this entry" the way `git show` does. The previous _content_ version is the baseline (state changes are skipped; their definition is identical), the first version diffs from empty, and a state-change entry reports "no definition change". The diff uses the same aligned renderer and tints as `gaffer diff`, with any engine version, emit, or tracking change named above it. The arrow keys keep scrubbing the timeline underneath, so the diff re-renders entry by entry, walking a definition's evolution in place. `PgUp`/`PgDn` scroll a long diff; `esc`, `d`, or `q` closes back to the timeline. A baseline on an older page is fetched automatically.

  **Out-of-band edits are flagged only once gaffer is managing the projection.** A content change reads as an out-of-band edit when a gaffer write precedes it, so a server that never round-trips gaffer's tool metadata (such as the V2 projection engine) reads neutrally rather than labelling every write "edited externally", as do edits made before gaffer took over. In `--json` the out-of-band flag is `outOfBand`, true for any non-gaffer write once gaffer has been managing the projection.

  Against a KurrentDB without the deploy-metadata field it degrades to the history with timestamps and content hashes only.

- bbfd56c: `gaffer rollback <projection> <hash>` rolls a deployed projection back to a prior version from its history, stamped `operation: rollback` in the deploy ledger. The target is named by its content hash from `gaffer history`; any unique prefix of 4 or more characters works. It confirms first with the current-to-target query diff (`--yes` skips). The apply is in place: processing continues from the current checkpoint, and local files stay untouched, so `gaffer diff` shows the rollback as drift until local is reconciled. A version differing in engine version or emitted-stream tracking is refused, pointing at `gaffer recreate`.

  The `gaffer history` timeline gains `r`: it opens the same confirm as a modal for the selected entry, applies on `y`, and reloads the timeline so the new rollback entry appears on top.

- bbfd56c: `gaffer recreate <projection>` destroys and rebuilds a deployed projection from local config: stop it, delete it (with its state and checkpoint streams), and create it fresh, reprocessing from zero. It applies a create-only change that deploy can't make in place (engine version, track-emitted-streams), or rebuilds a wedged projection an in-place reset can't fix. The projection is compiled before anything is deleted, so a broken local definition can't leave you with nothing to rebuild; `--no-validate` skips that check (production refuses it). It always confirms, more prominently against production, with `--yes` for non-interactive use. `--delete-emitted` also wipes the emitted streams so the rebuild doesn't re-emit duplicates.

  KurrentDB deletes projections asynchronously, so the rebuild's create waits out a ten-second settle window, retrying if it bounces off the still-registered name with a Conflict. Without it a slow delete could leave the projection deleted but not recreated; if the window does expire, the failure carries the recovery instructions.

- bbfd56c: `gaffer enable`, `gaffer disable`, and `gaffer delete` manage deployed projections on an environment, named directly (they need not be in `gaffer.toml`).

  - `gaffer enable <projection>` starts a projection so it resumes from its last checkpoint.
  - `gaffer disable <projection>` stops it, writing a final checkpoint; `--abort` skips that checkpoint so a later enable replays from the last one. Disabling is recoverable, so it confirms only against production.
  - `gaffer delete <projection>` removes the projection with its state and checkpoint streams, keeping emitted streams unless `--delete-emitted` is passed. It always confirms, and disables the projection first since the server won't delete an enabled one.

  `--yes` skips the confirmation; without a terminal (or with `--json`) a guarded verb won't proceed unconfirmed. Production gets a louder confirm, resolved from the server's `$server-info` or an explicit `production = true` on the env, never inferred from the environment's name.

- bbfd56c: A `production = true` flag on an `[env.<name>]` block marks the environment's database as production, activating the production guard tier locally. Deploy and operate confirmations name the target as production, and `--no-validate` is refused.

  - The flag combines with the database's own `$server-info` declaration as an OR, so it is opt-in only. `production = false` (the same as omitting it) defers to the server, and config can never downgrade a database that declares itself production. This activates the guardrail for the production databases that don't populate `$server-info` yet.
  - Confirmation prompts and messages now name the resolved environment when the server doesn't report a cluster name, including runs on the default environment, which previously showed no target name.
  - The history timeline gates like `gaffer rollback` does: its footer carries a production badge, and the rollback confirm names the production target.

- bbfd56c: The language server now serves deployment status for `gaffer.toml`, so editors surface it without reimplementing the fetch. On open or save it reads each environment's projection drift and runtime state in-process, reusing the same drift and target reads as `gaffer status`. Editors opt in via a `statusLens` initialization option, so all of this is a no-op for clients that don't render it.

  **Per environment**, a CodeLens above each `[env.<name>]` block rolls up how that environment's projections compare to local config, offers a sign-in action when the environment needs authentication, or shows a muted note when the read can't complete. A **Deploy** lens leads each reachable `[env.X]` block ahead of the roll-up, carrying the env and the declaring `gaffer.toml` so an editor can open the deploy plan for the whole project against that env. It's offered only when the env's status resolved and is authenticated, not while a fetch is in flight, on a fetch error, or when sign-in is needed.

  **Per projection**, a **Manage...** CodeLens above each `[[projection]]` header carries the projection and its configured environments, each environment's production flag, and the projection's runtime state, so an editor can render an action menu that offers pause-vs-resume and picks the right confirmation tier. Each environment's entry carries a `loading` flag while its status fetch is still in flight, so a client can show a spinner and settle into the resolved actions in place. Hovering the header lists each environment's drift verdict and runtime state, one per line with a colored status dot, and the server emits each environment's health on the header in file order for editors to render as a row of inline badges.

  **Refreshes stay cheap.** When a client polls for freshness the server refreshes only live runtime state with a cheap read, reusing the cached drift verdict; the verdict is recomputed only when a drift input actually changed. A local change is caught by file watching, either the config saved or a projection's source file edited. A server-side change is caught by a subscription to each projection's definition stream, so a deploy from outside the editor (the CLI, CI, or another tool) is reflected the moment it lands. The subscriptions are held only for open `gaffer.toml` files, and the timer borrows the same connection for its runtime read instead of dialing every tick. The server also refreshes when the editor signals an out-of-band auth change via `gaffer/refreshStatus`, such as a sign-in completing.

- bbfd56c: The language server now serves the projection actions editors need over its warm per-environment connection, so an editor no longer spawns a `gaffer` process per action, and advertises which of them it supports.

  - **`gaffer/operateProjection`** runs an operate verb (pause / resume / abort / delete) on a projection.
  - **`gaffer/diffVersions`** diffs any two versions of a projection, each a content hash, `deployed`, or `local`. It uses the same builder as `gaffer diff --left --right`, so the result matches the CLI's `--json` shape. `gaffer diff` itself is unchanged.

  Both are gated on the same `statusLens` initialization option as the deployment-status lenses.

  **The server lists the `gaffer/*` requests it serves** in its initialize result, under `capabilities.experimental.gaffer.methods`. LSP has standard capability fields for standard features but no slot for a server's own requests, so an editor extension driving an older `gaffer` previously had no way to discover support except to send a request and read `MethodNotFound` off the failure, by which point the user had already clicked something. Editors can now hide an action their CLI can't run.

- bbfd56c: The MCP server gains the deploy and projection-management tools, so an assistant can read a deployment and change it with a human in the loop.

  **Read-only**, mirroring the CLI's machine output:

  - **`deploy_status`** shows each projection's runtime state and drift verdict on an environment, plus any `[database_config]` divergence, like `gaffer status --json`.
  - **`deploy_plan`** previews what a deploy would change without applying anything, like `gaffer deploy --dry-run --json`.
  - **`deploy_history`** reads a projection's per-deploy audit log with paging, like `gaffer history --json`.

  **Writes:**

  - **`deploy_apply`** deploys projections from `gaffer.toml` like `gaffer deploy`, with the same all-or-nothing compile and diagnostics preflight (no validation bypass), the same per-item results as `gaffer deploy --json`, and every write stamped `operation: deploy` in the ledger.
  - **`deploy_pause`** / **`deploy_resume`** / **`deploy_abort`** mirror `gaffer disable` / `enable` / `disable --abort`.
  - **`deploy_recreate`** rebuilds from local config like `gaffer recreate`, gated on the compile and diagnostics preflight, and stamps `operation: recreate`.
  - **`deploy_rollback`** redeploys a prior version by content hash from `deploy_history`, like `gaffer rollback`, and stamps `operation: rollback`.
  - **`deploy_delete`** mirrors `gaffer delete`, including `deleteEmitted`.

  All tools accept an `env` argument and default to the default environment; `deploy_status` and `deploy_plan` echo the resolved target and production flag.

  **Confirmation is answered through the MCP client** (elicitation) and the assistant cannot answer it. A write against a production target, whether the server reports itself as production or the env sets `production = true`, requires one: the prompt front-loads `PRODUCTION [env.<name>]:`, states each verb's consequence, and for a deploy names the changed projections, rebuilds, out-of-band overwrites, faulted targets, and any `[database_config]` divergence. Recreate and delete destroy state with no undo, so they ask every time; on production their confirmation requires typing the projection name, and a production deploy plan containing `resetOnLogicChange` rebuilds requires typing the environment name. A client without elicitation support cannot perform gated writes; the refusal names the CLI command to run instead.

- bbfd56c: The deploy and projection-management commands now report the same anonymous usage telemetry as the rest of the CLI. `gaffer deploy`, `status`, `diff`, `history`, `rollback`, `recreate`, `enable`, `disable`, and `delete` each record which command ran and how it finished, including when a guardrail refuses the run. The mutating commands also record whether the target is production. `deploy` and `recreate` record whether validation was skipped, `deploy` whether the run was a dry-run, and `history` whether a rollback was applied from the timeline. Only buckets, booleans, and outcomes are collected. Projection names, connection strings, and content hashes never are. Opt out with `gaffer config telemetry off` or `telemetry = false` in `gaffer.toml`.
- 9b1f7e8: The Go runtime bindings no longer risk a rare fatal crash (`invalid pointer found on stack`) under GC pressure. The runtime's integer session handles could appear in pointer-typed stack slots during FFI calls; if the GC moved the stack while a callback was running mid-call, the process aborted. Handles now stay integer-typed end-to-end on the Go side, with all casting done in C shims. The crash could hit anything embedding the bindings, including the CLI.
- ee1f52e: Projection errors that reach the CLI wrapped in another error now keep their original error code and diagnostics. Previously a wrapped feed error was classified as `unexpected-error` and its diagnostics were dropped.
- ad9aaa7: Connection failures now name the resolution stage that failed (reading the env overlay, expanding the connection string). A certificate environment with multiple problems reports them in resolution order, so a broken `${VAR}` in a cert path surfaces before the TLS check that would follow it.
- f1c46c3: `gaffer dev --debug` no longer hangs when a Restart arrives as the session is tearing down; the restart returns cleanly during shutdown instead of leaving the debug adapter's read goroutine waiting forever.
- b2d6a66: Racing debug commands can no longer wedge a debug session. When two resume verbs raced on a paused projection (a double-clicked continue, or the MCP auto-step racing a user step), the loser's command could be queued just as the engine resumed. Its caller then blocked forever, and the stale command silently resumed the next breakpoint instead. The runtime now makes the enqueue atomic with the resume, fails commands that lost the race with an error instead of stranding them, and never carries queued commands across a pause.
- 17f9ea0: Commands that fail because an environment needs an interactive sign-in now exit with code `4` (distinct from the generic `1`), so a caller can offer a sign-in rather than parsing the error text. This is what the VS Code **Diff against deployed** action keys off to surface its one-click sign-in.
- ce0de95: Projection info now reports whether a projection writes events. `ProjectionInfo` gains an `emitsEvents` flag, true when the projection calls `emit`, `linkTo`, `linkStreamTo`, or `copyTo`. It is detected on every compile from the source, so consumers no longer need to inspect shape counts.

  `gaffer info` shows it ("Emits events: yes"); `gaffer info --json` and the MCP `get_projection_info` and `validate_projection` tools include `emitsEvents`; the testing library exposes it as `info.settings.emitsEvents`.

- 17f9ea0: Commands that connect to KurrentDB now give up faster when an environment is unreachable: 2 node-discovery attempts instead of the client library's 10, which cuts a failed connect from ~7s to ~1s. A reachable endpoint connects on the first attempt, so this only shortens the unreachable case. Set `maxDiscoverAttempts` in the connection string to override.
- eeaff5f: Event-processing errors under `engine_version 2` now carry the `quirk.handlerError.wedgesOnV2` (error severity) diagnostic. On deployed V2, an exception thrown while processing an event never faults the projection. It wedges silently: `status` stays `Running` while processing and persistence have stopped, and nothing is logged. Gaffer keeps faulting the event locally, which is the behaviour V2 should have, and the diagnostic rides the error to explain the divergence. It fires for any event-processing throw (handler, state load, `$created`, `$deleted`, state serialization, timeout); a throwing `partitionBy` is exempt because the server computes partition keys on its read loop, which faults properly.
- e2bf88d: The `gaffer init` template's comment now says at most one environment may be the default; a config with none is valid (`--env` or the interactive picker selects instead).
- 95a5410: `gaffer dev --json` now exits non-zero if it fails to write its output stream (for example a broken pipe to the editor), instead of silently finishing with a truncated stream.
- f52a6ac: The KurrentDB Go client is bumped to v1.4.1, fixing a nil-pointer panic when listing projections. Projection status reads go through this path; the client could dereference a nil stream on a failed request or a statistics frame with no details, which the status read caught as an unexpected failure. The client now returns a clear error in both cases instead of panicking.
- bbfd56c: The published packages declare keywords, so they surface in registry search.

  Two corrections to the published typings land alongside:

  - `@kurrent/gaffer-runtime`'s error hierarchy and `EventContext` now carry class-level JSDoc.
  - `@kurrent/projections-testing`'s `feed()` hover doc no longer claims thrown errors carry `input` and `normalized` fields. Nothing attaches them.

- 40d8b96: The OAuth token store honours a new `GAFFER_KEYRING_NAME` environment variable: when set, the encrypted-file fallback lives at `<user-config>/keyring-<name>` instead of the shared default, isolating a client's store on a host with no OS keyring. The name is sanitized to a single safe path segment; the OS-keyring path is unaffected.
- 76b126e: OAuth tokens are now bound to the host the environment's connection names, and gaffer only ever sends a token to the host it was obtained for. Previously a stored token was shared across every environment declaring the same issuer and client ID. A `gaffer.toml` reusing an org's issuer/clientID but pointing its connection elsewhere would therefore receive the user's real bearer token on any connect. An environment naming a different host now finds no token and asks for a fresh `gaffer auth` against that host instead.

  Environments pointing at the same host still share one sign-in, including across projects. Each environment's OAuth settings (such as `ca_file`) now always apply to its own connections, instead of reusing whichever environment connected first in a long-running process. `gaffer auth` now resolves the environment's connection before the browser flow and names the bound host in its success message. A connection string that can't be expanded or parsed fails the sign-in, since there is no host to bind the token to. Existing stored tokens are keyed the old way and won't be found; sign in once per host (or `gaffer auth --clear` first to tidy the keyring).

- b2071d0: A per-projection config error (e.g. `track_emitted_streams` with `engine_version 2`) no longer blocks every command. Previously one misconfigured projection failed `gaffer.toml` loading outright, so `gaffer info <good-projection>` died on an _unrelated_ projection's error. Now config validation splits into structural checks (environments, duplicate names) that stay fatal, and per-projection checks that are deferred; a bad projection only blocks operations on itself. The inspection commands (`status`, `diff`, `info`) show it as `invalid` through one shared rendering; `deploy` refuses just that one; `recreate` and the operate verbs fail only when you name it. Mirrors the per-projection degradation already used for compile errors.
- 3ad74db: Session teardown in the debug surfaces no longer races in-flight debug commands. Stopping an MCP run or ending/restarting a DAP debug session could free the native projection session while a step or resume from another goroutine was still executing inside it. That use-after-free could crash the process. The engine runner now refuses new session calls once teardown begins and waits out the in-flight ones before freeing the session.
- 64e1e84: OAuth environments no longer force a spurious re-sign-in when a command's connection and its config-drift check refresh the stored token at the same time. The two now share one refreshing token source per identity. A rotating identity provider (Auth0's reuse detection is the common case) can no longer reject one refresher's token as reused and discard a credential the other just rotated in.

  As a side effect, the config-drift check now shares the connection's unlocked credentials on a file-keyring host, where it previously skipped when it couldn't unlock the keyring on its own.

- e59a851: `track_emitted_streams` with `engine_version 2` is now reported as a diagnostic rather than a config-load error. The runtime emits `quirk.trackEmittedStreams.unsupportedOnV2` (error severity) off the resolved definition, whether the flag comes from `gaffer.toml` or `options({ trackEmittedStreams: true })` in the source. This matches how the other V2 incompatibilities (bi-state, `outputState`) already surface.

  `gaffer info`, `gaffer dev`, and `gaffer diff` now compile such a projection and show its full analysis plus the flag, instead of failing with a bare config error. `gaffer deploy` and `gaffer recreate` still refuse it at preflight (recreate before deleting anything), and the MCP `validate` tool reports it invalid with the diagnostic. The projection session no longer throws on the combination.

## 0.4.2

### Patch Changes

- 76a66a2: `gaffer dev --json` now emits an `auth_required` message when a live run can't authenticate without an interactive sign-in (no stored token, or a keyring that can't be unlocked non-interactively), instead of failing with a generic connection error. The VS Code extension uses this to offer a "Sign in" action that runs `gaffer auth` for you.
- 502a951: Gaffer can now authenticate to KurrentDB with an X.509 user certificate. Set `user_cert_file` and `user_key_file` on an `[env.<name>]` block. The paths expand `${VAR}` references and resolve relative to the project root, so a relative path works from any directory. The certificate requires a TLS connection and can be combined with OAuth.
- dffc3fc: Gaffer can now authenticate to KurrentDB with OAuth/OIDC bearer tokens. An `[env.<name>.oauth]` block configures the issuer and client ID. For interactive use, `gaffer auth --env <name>` signs in through the browser and stores a token that refreshes automatically; `gaffer auth --clear` removes stored tokens. For CI, setting `KURRENTDB_OAUTH_CLIENT_SECRET` selects the non-interactive client-credentials grant. `GAFFER_NO_OPEN` prints the sign-in URL instead of opening a browser, and `GAFFER_KEYRING_PASSWORD` supplies the keyring passphrase where there's no terminal to prompt on.
- fef0f6f: gaffer now discards a stored OAuth token the identity provider has rejected (`invalid_grant`) and re-prompts for sign-in, instead of surfacing it as a generic connection failure. In the VS Code extension the "Sign in" action re-appears on the same run.
- ce3de42: `gaffer dev --json` now emits a `run_error` message when a run ends on a connection failure (a dropped subscription or a failed connect), carrying the reason. Previously this only reached the output as plain stderr text. The VS Code extension uses it to show the failure as a notification and reflect it in the status panel, instead of a silent failure or a bare exit code.

## 0.4.1

### Patch Changes

- 6648f57: `gaffer.toml` is now written atomically (temp file + rename) instead of rewritten in place. A reader that re-reads the manifest on change (the LSP file watcher, the MCP server) can no longer catch a half-written file, and a crash mid-write can no longer truncate it.
- abca69e: `gaffer dev`, the MCP tools (`get_state`, `run`, `debug`), and the DAP `gaffer/partitionState` request now surface state-getter errors instead of silently returning partial or empty state. A throwing V1 `transformBy`/`filterBy` during state collection previously looked identical to an absent value. `get_state` now returns a tool error, `run`/`debug` results carry a `stateError` field when state collection fails, the DAP partition-state request returns an error response, and `gaffer dev` prints a `warning: reading projection state: ...` line while still showing the summary.
- 9948be5: `gaffer dev` now rejects contradictory source flags instead of silently dropping one. An offline source (`--fixture` / `--events`) can't be combined with a live target (`--env` / `--connection`). Previously `gaffer dev p --fixture happy --env cloud` ran the fixture and ignored `--env`. `--env` and `--connection` may still be combined, where `--connection` overrides `--env`.
- 592db24: The startup `.env` auto-load no longer walks above `$HOME` to find the project root. A stray `gaffer.toml` in a shared ancestor (a world-writable `/tmp`, or `/home` on a multi-user host) could otherwise make its `.env` (including `KURRENTDB_USERNAME` / `KURRENTDB_PASSWORD`) ambient for every `gaffer` invocation below it. The walk now stops at `$HOME`, matching the telemetry opt-out walk; the telemetry project-id walk is bounded the same way.
- 683e9e5: `gaffer mcp` no longer crashes when a session is torn down while a tool call is in flight. Concurrent tool calls that race a session teardown (for example `stop` while a `run` is parked at a breakpoint) previously could panic the whole MCP server or use-after-free the native session. Teardown is now serialised, a parked handler whose session was stopped returns a clean "session was stopped" error, and any residual handler panic is reported as a tool error instead of taking the process down.
- afafbff: `get_timeline` no longer fails with a raw `SQL logic error: no such table: steps` after a live `run`. The in-memory history store now pins itself to a single connection, so concurrent inserts from a live subscription and timeline queries always see the same database. When a session recorded no steps, `get_timeline` now reports "No timeline recorded for this session." instead of an empty range.
- 2374f05: A live `run` that times out before catching up no longer reports "timed out waiting for breakpoint" when no breakpoint was set. The `run` tool now names the actual condition (catching up to the head of the stream, hitting a breakpoint, or both), reports how many events were processed, and notes that the session is still running so it can be inspected with `get_state` / `get_timeline` or ended with `stop`.

## 0.4.0

### Minor Changes

- 33e3b4b: **Breaking:** `gaffer.toml` now models connections as named environments, and `engine_version` is set per projection. Top-level `connection` and top-level `engine_version` are no longer supported; loading a file with either fails with a migration hint.

  To migrate, move the top-level `connection` into an `[env.<name>]` block (mark one `default = true`), and set `engine_version` on each `[[projection]]`:

  ```toml
  # before
  connection = "kurrentdb://localhost:2113?tls=false"
  engine_version = 2

  [[projection]]
  name = "order-count"
  entry = "projections/order-count.js"

  # after
  [env.local]
  connection = "kurrentdb://localhost:2113?tls=false"
  default = true

  [[projection]]
  name = "order-count"
  entry = "projections/order-count.js"
  engine_version = 2
  ```

  Each `[env.<name>]` carries its own `connection`, and exactly one may set `default = true` (used when `--env` is omitted). Environment names must match `^[A-Za-z0-9_-]+$`.
  - `gaffer dev` gained `--env <name>` to select an environment; `--connection` is an ad-hoc override that beats both `--env` and the configured environment. The MCP `list_events` and live `run` tools take the same `env` argument.
  - A per-environment `.env.<env>` file overlays the base `.env`, so each environment can carry its own credentials. The precedence, highest first, is the shell environment, then `.env.<env>`, then the base `.env`. Both `${VAR}` references in a connection and the `KURRENTDB_USERNAME` / `KURRENTDB_PASSWORD` credentials resolve from those sources.
  - `gaffer init` no longer takes `--engine-version` or `--yes`; it writes a commented starter template.

### Patch Changes

- 327fc30: `gaffer dev` resolves event sources more helpfully when `gaffer.toml` defines environments. The interactive source picker now offers each configured environment as a live option, not just the `default` one, so a single non-default environment is selected automatically and multiple are pickable. When no source resolves non-interactively, the error names the available environments and suggests `--env <name>` or `default = true`, rather than pointing you to configure an `[env.<name>]` you may already have.
- 3324def: `.env` is now loaded into the process environment at startup, so a project `.env` applies on every code path, not only after a database connection is made.
  - Env-var opt-outs (`GAFFER_TELEMETRY_OPTOUT`, `KURRENTDB_TELEMETRY_OPTOUT`, `DO_NOT_TRACK`, `GAFFER_NO_UPDATE_CHECK`) set in `.env` are now honoured. Previously they were read only from the shell environment.
  - The `connection` string in `gaffer.toml` supports `${VAR}` expansion (braced form only), so credentials can stay out of the committed file. An undefined variable is an error; a bare `$` is left untouched.
  - The shell environment wins over `.env`: a value already set in the shell, or injected by CI, is never overwritten.

- 33e3b4b: `gaffer scaffold` now lets you choose the new projection's engine version (`1` or `2`, default `2`). It's a `--engine-version` flag and an interactive prompt on the CLI, an `engine_version` argument on the MCP `scaffold` tool, and a step in the VS Code scaffold wizard.

## 0.3.1

### Patch Changes

- 430c78d: `gaffer init`, `gaffer scaffold`, and `gaffer dev` now prompt interactively when run on a terminal, asking only for values not already supplied as flags or positionals.
  - `gaffer init` prompts for the engine version and gains an `--engine-version <1|2>` flag (default `2`).
  - `gaffer scaffold` prompts for the path (when omitted) plus source, partitioning, and emit, offering only partitioning options valid for the chosen source.
  - `gaffer dev` prompts for the projection (when omitted) and the event source when none is given via `--events` / `--fixture` / `--connection`.
  - `gaffer scaffold` and `gaffer dev` gain `--yes` / `-y` to skip prompts (the projection path / name must then be supplied as arguments). On `gaffer init`, `-y` now skips the prompt and uses the default engine version, rather than being a no-op.
  - `gaffer scaffold` now rejects per-stream partitioning on a single-stream source up front, instead of generating a projection that only fails when run.

  Piped and non-interactive (CI) invocations are unchanged: they never prompt, so existing scripts keep working.

## 0.3.0

### Minor Changes

- 9f9722a: Diagnostic codes now use one `quirk.*` / `usage.*` taxonomy. Every diagnostic has a three-segment code `<class>.<subject>.<detail>`, where `quirk.*` reproduces a KurrentDB engine bug and `usage.*` flags something about your own projection.

  This is a breaking rename of the diagnostic codes surfaced on `FeedResult.diagnostics`, `ProjectionInfo.diagnostics`, the testing library's `step.diagnostics`, and the CLI/MCP output:
  - `compat.linkStreamTo.outOfBoundsParameters` → `quirk.linkStreamTo.outOfBoundsParameters`
  - `compat.log.multiParam` → `quirk.log.multiParam`
  - `compat.event.bodyCast` → `quirk.event.bodyCast`
  - `compat.serialize.nonFinite` → `quirk.serialize.nonFinite`
  - `compat.transforms.notInvoked` → `usage.transforms.notInvoked`
  - `compat.outputState.unconditional` → `quirk.outputState.noEffectOnV2`
  - `deprecated.linkStreamTo` → `usage.linkStreamTo.deprecated` (now Information, was Warning)
  - `options.duplicate` → `usage.options.duplicate`
  - `handler.async` → `usage.handler.async`
  - `handler.promise` → `usage.handler.promise`

  Other changes in this release:
  - **Severity is Error / Warning / Information only.** The unused `Hint` level (LSP 4) is dropped from `DiagnosticSeverity`. Severity follows a per-firing rubric: Error when there is no correct form (it throws or is unsupported), Warning when it runs but produces a wrong result, Information when it works but is noteworthy.
  - **`reorderEvents` is engine-version aware.** Under `engine_version 1`, an invalid reordering config (not `fromStreams()` with 2+ streams, or `processingLag` below 50ms) is rejected at session create, matching KurrentDB's `ReaderStrategy`. Under `engine_version 2` it has no effect and surfaces as a `usage.reorderEvents.noEffectOnV2` warning rather than the old unconditional error. This replaces the `options.fromStreamsOnly` diagnostic.
  - **Throwing quirks now also raise a diagnostic.** A quirk that throws (e.g. `quirk.event.bodyCast`, `quirk.serialize.nonFinite`) exposes a `diagnostics` array on the thrown error, surfaced on the Go error types and the JS `ProjectionError` and propagated through the testing library. The array carries the quirk that threw plus any that fired earlier in the same event, so it is the complete set where `compatCode` is just the throwing quirk's code. Errors are also enriched with `compatDescription` and `compatFixedIn`.
  - **Quirk-catalogue exports are removed.** The catalogue is no longer exported over FFI: `knownQuirks()` (and the `KnownQuirk` type) is gone from the JS runtime binding, and `KnownQuirks()` / `KnownQuirk` / `DiagnosticSeverityHint` are gone from the Go binding. Assert on `step.diagnostics` (the data plane) instead.
  - **Diagnostics trued up against KurrentDB 26.2.0 (PR #5610).** `quirk.event.bodyCast` and `quirk.serialize.nonFinite` are marked fixed in 26.2.0 and no longer fire when targeting that version. The `biState.stringSlot` / `biState.sharedStringSlot` quirks are **removed**: JSON-encoding a string state-array slot is correct KurrentDB behaviour, not a bug. The real bug is the new `quirk.serialize.rawString`: a bare string state that isn't valid JSON is persisted un-encoded and faults on reload (also fixed in 26.2.0).
  - **New `engine_version 2` diagnostics.** `quirk.biState.sharedStateResetOnV2` flags bi-state / `$initShared` projections on V2, where shared state is silently re-initialized on restart. `trackEmittedStreams` on V2 is rejected at session create, matching KurrentDB. `outputState()` on V2 is now `quirk.outputState.noEffectOnV2` (Warning, was `usage.outputState.unconditional` Information). V2 emits no result streams, with parity planned for a future release.

- e9dfaff: The quirks-selecting option and the quirk registry are renamed to retire the misleading "DB version" / "bug" framing.
  - **`dbVersion` is now `quirksVersion`** across the runtime, the JS bindings (`SessionOptions`), and the testing library (`ProjectionOptions`). The value is unchanged: a `MAJOR.MINOR.PATCH` string, where unset still reproduces every known quirk and a set version turns off quirks fixed upstream as of it. Only the key moves. `dbVersion` read as passive info when it actively selects which quirks to emulate, and it collided with `engineVersion`.
  - **`knownBugs()` is now `knownQuirks()`**, and **`KnownBug` is now `KnownQuirk`**, in the JS and Go bindings. Most registry entries are deliberate KurrentDB quirks gaffer reproduces, not bugs to report upstream.
  - **CLI**: the `gaffer.toml` key `db_version` is now `quirks_version`, the env var `GAFFER_DB_VERSION` is now `GAFFER_QUIRKS_VERSION`, and the MCP resource `gaffer://docs/db-version-bugs` is now `gaffer://docs/quirks`. The connected-server-version telemetry (the `db_version` event property) is unaffected, since it genuinely reports the connected DB version.

  No deprecation period: pre-1.0, hard break. An old `dbVersion` or `db_version` key is silently ignored rather than rejected, so update existing call sites and `gaffer.toml` files.

### Patch Changes

- cf26d46: Projection handlers that use `async` or return a `Promise` now produce a compile-time warning. The projection engine is synchronous (no event loop), so it serializes the returned `Promise` as the state instead of awaiting it, leaving the state as `{}`. This matches KurrentDB but is surprising when authoring tests in an async-capable JS runtime, so gaffer flags it. The `Promise` check is a literal-syntax heuristic (`new Promise(...)`, `Promise.resolve(...)`, and similar).
- ad942bb: `gaffer scaffold`, `dev`, and `info` now report a missing or extra positional argument by naming the argument and showing a runnable example, instead of cobra's generic `Accepts 1 arg(s), received 0.`:

  ```
  missing required argument <path>
  example: gaffer scaffold ./projections/order.js
  ```

  Their `--help` gains an example too, and `dev`/`info` now show the required argument as `<projection>` rather than `[projection]`.

- 627dd02: `gaffer dev` now surfaces runtime quirks at the point they fire while processing an event, such as a `biState` string slot being JSON-quoted on persistence or a multi-argument `log()` call. In text output each quirk prints inline, interleaved with the handler's `log()` lines and emits in the order they happened, so stepping through a projection shows the warning as you hit the line. The JSON result line still carries a `diagnostics` array, and the run summary tallies every distinct quirk, compile-time and runtime alike. A `gaffer/stepWarning` DAP event also fires live per quirk, so editor integrations can attach the warning to the step.
- d59611f: The `gaffer dev` DAP `gaffer/stats` event now carries a `quirks` count: the number of distinct runtime-quirk codes seen so far in the session. This lets an editor tally fired quirks in its status view without tracking the per-step warnings itself.
- 652947b: Diagnostics now link to a reference page at [gaffer.kurrent.io/reference/diagnostics](https://gaffer.kurrent.io/reference/diagnostics/), generated from the diagnostic catalog with one entry per `quirk.*` / `usage.*` code. The `gaffer dev` and test summaries print a `See <url>` line after the quirk list, and on interactive terminals each diagnostic code is itself a hyperlink to its entry. The VS Code step-warning panel makes each quirk a clickable link too.
- 627dd02: `gaffer dev` text output now prints a handler's `log()` lines and emitted events under their own event header, in the order they happened, instead of before the header. The header is deferred until the result is known (so skipped events can be dropped silently), but logs and emits produced during processing now flush that header first.
- 9411111: The runtime and testing library now report three previously cryptic errors with friendlier messages:
  - `foreachStream()` on a `fromStream()` or `fromStreams()` projection now fails with "foreachStream() is only supported with fromAll() and fromCategory()", instead of a raw "Property 'foreachStream' of object is not a function" engine error.
  - A second `options()` call now produces a compile-time warning, since only the last call takes effect and the earlier ones are discarded silently.
  - The testing library now names which event shape was attempted and which field is wrong when a fed event matches none, instead of valibot's cryptic `Expected Object but received Object`.

- 2102508: The MCP server gains an `init` tool, so an assistant can create a gaffer project without leaving the protocol. Previously a project-less server could read the docs but had no in-protocol way to bootstrap one.
  - `init` creates a `gaffer.toml` in the server's project directory (the `--project` / `GAFFER_PROJECT` override, otherwise the working directory). The projection tools then resolve it on the next call, with no restart.
  - It refuses to run when a project is already in scope, naming where one was found, so it never shadows an existing project with a nested copy.
  - `gaffer init` and the tool now share one implementation, so they can't drift on what a fresh project looks like.

- 31a9b89: `gaffer mcp` can now be pointed at a project explicitly, instead of only searching upward from the working directory. This matters when the server is registered globally and launched from an arbitrary directory.
  - A `--project <dir>` flag and a `GAFFER_PROJECT` environment variable, each accepting a project root or any directory inside it (gaffer walks up to find the `gaffer.toml`).
  - Precedence: `--project` over `GAFFER_PROJECT` over the working-directory search.
  - When the override points somewhere without a `gaffer.toml`, the server still starts; the project tools' error names the path you gave so the misconfiguration is obvious.

- 6a441f8: `gaffer mcp` re-reads `gaffer.toml` on each project-dependent tool call instead of caching it for the session. Editing the manifest mid-session (adding a projection, fixing a connection string) is picked up by the next call with no restart; an invalid manifest surfaces a load error rather than silently serving the last good config.
- b0242e3: The MCP server now surfaces the runtime quirks that fired while processing an event, so an assistant can spot a fired quirk and act on it. `get_step` gains a top-level `diagnostics` array of the full quirk objects, and `get_timeline` / `get_history` carry the distinct quirk codes (`quirks`) per step. Each code cross-references the existing `gaffer://docs/quirks` resource, which explains the quirk and names a `quirksVersion` that opts out where one exists.
- 82b73f3: `gaffer mcp` now starts even when there is no `gaffer.toml` in the working directory, instead of failing during the MCP handshake. This makes the server safe to install as a global plugin, where the launch directory is arbitrary.
  - The documentation resources (`projection-api`, `gotchas`, `examples`, `quirks`) and `get_version` work without a project.
  - Project-dependent tools (`run`, `validate`, `list_projections`, `scaffold`, `get_projection_info`, `list_events`, debug) return a tool error pointing at `gaffer init` rather than taking the server down.
  - The project is resolved lazily, so creating a `gaffer.toml` mid-session is picked up on the next tool call without restarting the server.

  A `gaffer.toml` that exists but fails to parse or validate still surfaces as a startup error.

- c5d77a1: `gaffer mcp` usage telemetry now records a `started_in_project` flag, distinguishing sessions launched inside a project from project-less ones (for example a globally-registered server started outside any project).

  Manifest features are now also recorded for sessions that resolve their project lazily mid-run, for example after the `init` tool creates one. Previously those sessions left `manifest_features_used` unset.

- 1458673: `gaffer.toml` handling of `engine_version` has two fixes:
  - `gaffer scaffold` (and any command that re-saves the manifest) no longer writes `engine_version = 0` for projections with no engine version set. Previously the line was stamped on save, including onto existing projections.
  - An explicit `engine_version = 0` is now rejected with "must be 1 or 2, got 0" instead of being silently treated as unset.

- 47cfe96: Setting `reorderEvents` or `processingLag` on a projection whose source is not `fromStreams()` now produces a compile-time error diagnostic. These options only apply to `fromStreams([])`: KurrentDB rejects `reorderEvents` on other sources at subscription time, and `processingLag` has no effect without it. Gaffer previously accepted both on any source and silently ignored them.
- b217c5e: The runtime now builds with `InvariantGlobalization` enabled, so error messages stay English regardless of the host machine's locale. Previously a non-English-preference machine produced partially-translated framework messages (for example `... не число is not a valid JSON value` instead of `... NaN is not a valid JSON value`). These read as garbled text and made string-based test assertions non-portable across locales. The ICU dependency is also dropped from the native binary.

## 0.2.0

### Minor Changes

- 97cc7f0: `gaffer scaffold` now takes an explicit file path instead of a bare projection name. The bare-name form is gone; users must pass a path that ends in a supported extension (`.js` today).

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

### Patch Changes

- 3b5392c: Documentation links in the README now point at `gaffer.kurrent.io` rather than the `docs.kurrent.io/gaffer/` placeholder.
- d241c58: Drop the half-implemented `enabled` projection key from `gaffer.toml`. The key was honoured by exactly one consumer (MCP's `list_projections` tool, and only as an output tag rather than a filter); every other path - `gaffer dev`, `gaffer info`, `gaffer manifest`, the VS Code lens - ran and listed projections regardless.

  The TOML parser silently ignores unknown keys, so any `enabled = false` left in an existing `gaffer.toml` becomes a no-op rather than an error. MCP `list_projections` no longer tags disabled projections in its output.

- 64d77dc: `gaffer init` now creates only `gaffer.toml`. The empty `.gaffer/` directory and the `.gitignore` entries (`.env`, `.env.*`, `.gaffer/`) were speculative scaffolding: nothing in tree reads the directory, the gitignore patterns presumed too much about the user's project layout (e.g. `.env.*` would have caught `.env.example`), and `.gitignore` was being created even outside a git repo.
- 3707343: `gaffer init` is now non-interactive by default. Previously bare `gaffer init` errored out and pointed at a `--yes` flag the user had no reason to know about; running it now does what `-y` did before. The `--yes` / `-y` flag is parsed but currently a no-op, kept available for forward-compat with the upcoming interactive form.
- a68e3c8: `gaffer manifest` cleanups:
  - The command is now hidden from `gaffer --help`. Its audience is editor extensions and other wrappers that feature-gate their UI against a specific gaffer build, not interactive users.
  - The manifest now walks the full command tree and emits nested commands under composite keys (e.g. `config telemetry status`). Previously only direct children of the root appeared, so the `config` subtree was missing from the output. Non-runnable group commands (e.g. bare `config`) are traversed but not emitted: the manifest lists invocable commands, not navigation nodes.

- 723e35a: `gaffer manifest` now reports `updateAvailable: "x.y.z" | null` alongside `version` and `commands`. The value is sourced from the existing once-per-day update-notifier cache, so manifest fetches add no extra network call. Editor wrappers (the VS Code extension) can surface a one-click update toast without re-checking the npm registry.
- 95af1d2: MCP server gains two read-only introspection tools that mirror the CLI:
  - `get_projection_info` returns the same JSON shape as `gaffer info <name> --json` (parsed structure, sources, partition mode, emit declarations, effective engine version). The projection `name` is optional when the project defines exactly one projection.
  - `get_version` returns the gaffer CLI version string.

  Both are sync, no session state, and don't take a configured KurrentDB connection.

- 723e35a: `gaffer`'s update-check pipeline now separates the stderr notice from the registry refresh.
  - The "Update available" stderr notice is suppressed on machine-readable invocations: `gaffer manifest`, `gaffer lsp`, `gaffer mcp`, or any command run with `--json`. Previously the notice could print onto the sibling stream of a structured stdout payload when stderr was a TTY (e.g. `gaffer manifest | jq`).
  - The once-per-day registry refresh now runs on non-interactive paths too. Previously it was gated on the same TTY check as the notice, so a user invoking gaffer only through an editor wrapper would have a stale-forever cache. The refresh is still skipped under `--no-update-check` and `GAFFER_NO_UPDATE_CHECK=1`.

- 3707343: Restyle the first-mint telemetry disclosure to match the styled card used by `gaffer --help` and the update-available notice, and reword the copy so the lead names what the data is used for (feature prioritisation and bug fixing). `KURRENTDB_TELEMETRY_OPTOUT` and `DO_NOT_TRACK` remain honoured but no longer appear in the banner; the full reference is in `cli/TELEMETRY.md`.
- 09ea79b: Notify when a newer gaffer release is available. On interactive runs the CLI now prints a one-line stderr hint when the cached `latest` version on npm is ahead of the running binary, e.g.

  ```
  gaffer 0.2.0 available (you have 0.1.3). Update with: npm install -g @kurrent/gaffer@latest
  ```

  Notification only - the CLI never self-installs. A background once-per-24h GET against `https://registry.npmjs.org/@kurrent/gaffer/latest` refreshes the cache for the next run; the synchronous read at startup keeps the notice instant. Network failures, non-200s, and malformed responses are silent.

  Suppress with the `--no-update-check` flag or the `GAFFER_NO_UPDATE_CHECK=1` env var. The check skips itself when stderr isn't a TTY, so extension-spawned `gaffer lsp` / `gaffer mcp` / `gaffer manifest --json` invocations and CI runners never see the notice.

## 0.1.3

### Patch Changes

- aeed2b2: Mark the CLI binary as executable in published tarballs.

## 0.1.2

### Patch Changes

- d3d297a: Restore the executable bit on the CLI binary inside each per-platform native package. `actions/upload-artifact@v4` stores artifacts as zip, which drops unix permission bits - 0.1.1 shipped `gaffer` as `0644` so `npx @kurrent/gaffer` failed with `EACCES`. The runtime shared libraries (`.so` / `.dylib` / `.dll`) are unaffected; they load via dlopen and don't need `+x`. Windows resolves executability by `.exe` extension, so the win32 binary is also unaffected.

## 0.1.1

### Patch Changes

- 2675301: Republish the per-platform native packages with their compiled CLI binary and co-located runtime. 0.1.0 shipped those packages empty - `gaffer` exited with `native binary for <platform> not found` on every invocation. Same root cause as the runtime fix: a CI artifact-handling bug. Reinstall `>=0.1.1`.

## 0.1.0

### Minor Changes

- 5b85426: Develop, test, debug, and deploy KurrentDB projections from the command line. Runs projections locally against the same JavaScript engine KurrentDB uses.
  - `gaffer init` - create a new project (`gaffer.toml`, `.gaffer/`, `.gitignore`).
  - `gaffer scaffold <name>` - add a new projection.
  - `gaffer dev` - run a projection against fixtures, an events file, or a live KurrentDB instance.
  - `gaffer info` - inspect projection details.
  - `gaffer config` - manage user-level configuration (telemetry, identity).
  - `gaffer lsp` - Language Server Protocol over stdio for editor integration.
  - `gaffer mcp` - Model Context Protocol server exposing scaffolding, validation, debugging, and the projection API to AI agents.
  - `gaffer version` - print the installed version.
  - Debug Adapter Protocol server for breakpoint debugging, wired up automatically by the [KurrentDB Projections VS Code extension](https://marketplace.visualstudio.com/items?itemName=kurrent-io.gaffer).
