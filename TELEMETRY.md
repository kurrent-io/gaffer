# Usage telemetry

Gaffer collects anonymous usage statistics and sends them to Kurrent, Inc. when the tool is run. Telemetry data helps us refine and improve gaffer based on real usage patterns.

## What is usage telemetry

Gaffer telemetry only tracks non-Personally-Identifiable Information. Collected data does not allow Kurrent to fingerprint users by any of the collected data points.

Examples of the telemetry data collected:

- Gaffer version, host OS and architecture
- Whether gaffer is running locally or in CI
- Which gaffer command ran and how it finished
- Bucketed counts of work done (`none`, `1`, `2-9`, `10-99`, `100-999`, `1000+`)
- The structural shape of projection files (which builtins are called, with bucketed counts; which handlers are registered; bucketed file size)
- Whether the gaffer CLI is reachable when the VS Code extension activates
- Crashes in gaffer's own code (gaffer-authored error messages with scrubbed stack frames)

What gaffer **does not** track:

- Projection source code, in any form
- Stream names, event names, category names, result-stream patterns
- Function names, variable names, or any identifier from your code
- Error messages produced by your projection code
- File paths beyond basenames in scrubbed stack frames
- KurrentDB connection strings, hostnames, or credentials
- Environment variable names or values
- User account or OS user information
- IP addresses

There are four event types. Each is wrapped in an envelope alongside shared install metadata (gaffer version, host OS, architecture, the runtime environment - `local` or `ci`) before being sent. On a new install, the first envelope also includes the install date so we can group activity by cohort. When one gaffer process spawns another (typically the VS Code extension spawning the CLI), the spawned process additionally carries the parent's anonymous id so the two are recognised as one user.

The receiving worker stamps each event with its own deploy timestamp so we can correlate analytics against the server version that processed them.

The precise wire format lives in [`telemetry/schemas/events.cue`](https://github.com/kurrent-io/gaffer/tree/main/telemetry/schemas/events.cue) (event shapes) and [`telemetry/schemas/wire.cue`](https://github.com/kurrent-io/gaffer/tree/main/telemetry/schemas/wire.cue) (envelope).

### `command_invoked`

Records which gaffer command ran, what its outcome was, and bucketed counts of work done.

<details>
<summary>Example envelope</summary>

```json
{
	"schema_version": "1",
	"emitter_id": "8f2b1a4c-9e7d-4a3e-b5f2-7c8a9d4e1f02",
	"run_id": "01938e7a-3c8d-7e2f-bac3-8d4e2f1c9a07",
	"context": {
		"emitter": "cli",
		"lib_version": "0.4.2",
		"os": "linux",
		"arch": "x64",
		"runtime_environment": "local"
	},
	"events": [
		{
			"name": "command_invoked",
			"timestamp": "2026-05-08T12:34:56.000Z",
			"properties": {
				"command": "dev",
				"duration_ms": 100,
				"outcome": "user_interrupt",
				"invoked_by": "direct",
				"invoked_via": "terminal",
				"manifest_features_used": ["projections", "fixtures"],
				"projection_count": 10,
				"fixture_count": 2,
				"connected_to_db": true,
				"db_version": "26.1"
			}
		}
	]
}
```

</details>

### `projection_shape`

Records the source-mechanical shape of a projection file: which projection builtins are called (`fromAll`, `when`, `partitionBy`, etc.) with bucketed call counts, which handlers are registered, and a bucketed file size. The projection's identifier is a salted hash that's stable across runs but does not reveal the projection's path or contents.

<details>
<summary>Example envelope</summary>

```json
{
	"schema_version": "1",
	"emitter_id": "8f2b1a4c-9e7d-4a3e-b5f2-7c8a9d4e1f02",
	"run_id": "01938e7a-3c8d-7e2f-bac3-8d4e2f1c9a07",
	"context": {
		"emitter": "cli",
		"lib_version": "0.4.2",
		"os": "linux",
		"arch": "x64",
		"runtime_environment": "local"
	},
	"events": [
		{
			"name": "projection_shape",
			"timestamp": "2026-05-08T12:34:56.000Z",
			"properties": {
				"projection_id": "a1b2c3d4e5f6789a",
				"parsable": true,
				"file_size": 5120,
				"handlers": {
					"any": false,
					"init": true,
					"deleted": false,
					"distinct_event_names": 10
				},
				"builtin_counts": {
					"fromAll": 1,
					"when": 10,
					"partitionBy": 1,
					"emit": 100
				}
			}
		}
	]
}
```

</details>

### `extension_activated`

Records whether the gaffer CLI binary is reachable on the user's `PATH` when the VS Code extension activates - the "broken install" diagnostic - along with editor and CLI versions and a bucketed activation duration.

<details>
<summary>Example envelope</summary>

```json
{
	"schema_version": "1",
	"emitter_id": "0b51e34d-aac8-4cce-bce4-9d2c7c6e3b8a",
	"run_id": "01938e7a-1b2c-7d4e-9faf-2a3b4c5d6e7f",
	"context": {
		"emitter": "vscode",
		"lib_version": "0.4.2",
		"os": "darwin",
		"arch": "arm64",
		"runtime_environment": "local"
	},
	"events": [
		{
			"name": "extension_activated",
			"timestamp": "2026-05-08T12:00:00.000Z",
			"properties": {
				"editor": "vscode",
				"editor_version": "1.95.2",
				"cli_reachable": true,
				"cli_version": "0.4",
				"activation_duration_ms": 100
			}
		}
	]
}
```

</details>

### `exception`

Records crashes in gaffer's own code (Go panics in the CLI, unhandled JS errors in the extension host, runtime exceptions in the projection engine). Exception messages are always written by gaffer and never propagated from your projection code. Stack frames are scrubbed structurally: file basenames only, user-JS frames dropped entirely.

<details>
<summary>Example envelope</summary>

```json
{
	"schema_version": "1",
	"emitter_id": "8f2b1a4c-9e7d-4a3e-b5f2-7c8a9d4e1f02",
	"run_id": "01938e7a-3c8d-7e2f-bac3-8d4e2f1c9a07",
	"context": {
		"emitter": "cli",
		"lib_version": "0.4.2",
		"os": "linux",
		"arch": "x64",
		"runtime_environment": "local"
	},
	"events": [
		{
			"name": "exception",
			"timestamp": "2026-05-08T12:34:56.000Z",
			"properties": {
				"exceptions": [
					{
						"type": "RuntimeError",
						"value": "failed to load runtime library",
						"in_app": true,
						"stacktrace": {
							"type": "raw",
							"frames": [
								{
									"filename": "engine.go",
									"function": "Run",
									"lineno": 123,
									"in_app": true
								}
							]
						}
					}
				],
				"command": "dev",
				"phase": "startup"
			}
		}
	]
}
```

</details>

## Disclosure

On the first invocation, gaffer prints a one-time message to the terminal, similar to:

```
Telemetry
---------
Gaffer collects usage data in order to improve your experience. The data is anonymous and collected by Kurrent, Inc.

You can opt out by any of:
  - Running `gaffer config telemetry off` (this machine)
  - Adding `telemetry = false` to your project's gaffer.toml
  - Setting GAFFER_TELEMETRY_OPTOUT, KURRENTDB_TELEMETRY_OPTOUT, or DO_NOT_TRACK to a truthy value

For more information visit https://telemetry.gaffer.kurrent.io.
```

The VS Code extension shows an equivalent notification on first activation, with `[Disable]`, `[Learn more]`, and `[Dismiss]` buttons.

If telemetry collection has already been disabled (for example via `KURRENTDB_TELEMETRY_OPTOUT` carried over from KurrentDB, or `DO_NOT_TRACK`), no disclosure is shown - the user has already declined.

## Reporting frequency

Gaffer emits events at the boundary of work, not on a periodic schedule:

- `command_invoked` is sent once per CLI invocation, when the process exits.
- `projection_shape` is sent the first time gaffer encounters a projection file in a process, and again only if the file's bucketed shape changes.
- `extension_activated` is sent once when the VS Code extension activates.
- `exception` is sent when gaffer's own code crashes.

A gaffer process that does no work emits nothing. There is no periodic heartbeat.

## How to opt out

Telemetry transmission can be disabled by any one of the following:

- Add `telemetry = false` at the top of `gaffer.toml`. Covers every gaffer invocation in that project, including CI.
- Run `gaffer config telemetry off`. Covers every gaffer invocation by this user on this machine.
- Set `GAFFER_TELEMETRY_OPTOUT`, `KURRENTDB_TELEMETRY_OPTOUT`, or `DO_NOT_TRACK` to a truthy value (`1`, `true`, `yes`, `on`).
- Set VS Code's `telemetry.telemetryLevel` to anything other than `all`. The gaffer extension respects this setting.

When opted out, gaffer does not collect telemetry. No envelope is constructed and no event is recorded locally.

## Where data is stored

Telemetry data is stored in PostHog's EU instance. Envelopes transit Cloudflare's edge network on the way there. Cloudflare's standard request logs include IP and are retained for around 30 days; gaffer does not forward IPs to PostHog. The worker that handles ingest is open source and lives in the [gaffer repository](https://github.com/kurrent-io/gaffer/tree/main/telemetry/worker), alongside the [machine-readable schema](https://github.com/kurrent-io/gaffer/tree/main/telemetry/schemas) for the events described above.

## How to delete your data

Email `privacy@kurrent.io` with the identifier gaffer prints below. All events associated with that id are deleted from PostHog within 30 days. Session-stitching and identity-merge rows the worker holds for that id expire automatically within 25 hours and 30 days respectively.

To find your id:

- `gaffer config telemetry status` prints it while you are opted in.
- `gaffer config telemetry off` prints it one last time before clearing local state.

The same identifier is what every gaffer telemetry envelope carries on the wire and what PostHog stores as the person id.
