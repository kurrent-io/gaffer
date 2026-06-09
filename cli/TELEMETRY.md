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
- Which gaffer diagnostics fired during a `gaffer dev` run (the diagnostic codes only - gaffer's own `quirk.*` / `usage.*` identifiers, e.g. `quirk.serialize.rawString`; never your code or counts)
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

There are three event types. Each is wrapped in an envelope alongside shared install metadata (gaffer version, host OS, architecture, the runtime environment - `local` or `ci`) before being sent. When gaffer runs inside a project, the envelope also carries a salted hash of the project's root path (`project_id`). The path itself never leaves your machine, only the hash. When the gaffer CLI is launched by another gaffer process (typically the VS Code extension), the spawned CLI additionally carries the parent's anonymous id.

The receiving worker stamps each event with its own deploy timestamp.

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
				"db_version": "26.1",
				"diagnostics_seen": ["quirk.serialize.rawString", "usage.handler.async"]
			}
		}
	]
}
```

</details>

### `projection_shape`

Records the shape of a projection file: which projection builtins are called (`fromAll`, `when`, `partitionBy`, etc.) with bucketed call counts, which handlers are registered, and a bucketed file size. The projection's identifier is a salted hash that's stable across runs but does not reveal the projection's path or contents.

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

### `exception`

Records crashes in gaffer's own code (Go panics in the CLI, runtime exceptions in the projection engine). Exception messages are always written by gaffer and never propagated from your projection code. Stack frames are scrubbed: file basenames only, user-JS frames dropped entirely.

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
Gaffer sends anonymous usage data and error reports
to help us prioritise features and fix bugs faster.

To opt out
 This machine: gaffer config telemetry off
 This project: telemetry = false in gaffer.toml
 Env var:      GAFFER_TELEMETRY_OPTOUT=1

Details: https://gaffer.kurrent.io/telemetry/
```

`KURRENTDB_TELEMETRY_OPTOUT` (carried over from KurrentDB) and `DO_NOT_TRACK` (industry convention) are also honoured silently - users who set them in another context get the same effect without needing to know gaffer reads them. If telemetry has already been disabled by any of these signals, the disclosure is not shown.

## Reporting frequency

Gaffer emits events at the boundary of work, not on a periodic schedule:

- `command_invoked` is sent once per CLI invocation, when the process exits.
- `projection_shape` is sent the first time gaffer encounters a projection file in a process, and again only if the file's bucketed shape changes.
- `exception` is sent when gaffer's own code crashes.

A gaffer process that does no work emits nothing. There is no periodic heartbeat.

## How to opt out

Telemetry transmission can be disabled by any one of the following:

- Add `telemetry = false` at the top of `gaffer.toml`. Covers every gaffer invocation in that project, including CI.
- Run `gaffer config telemetry off`. Covers every gaffer invocation by this user on this machine.
- Set `GAFFER_TELEMETRY_OPTOUT`, `KURRENTDB_TELEMETRY_OPTOUT`, or `DO_NOT_TRACK` to a truthy value (`1`, `true`, `yes`, `on`). Read from your shell environment or a project `.env` file.

When opted out, gaffer does not collect telemetry. No envelope is constructed and no event is recorded locally.

## How to see what's being sent

Set `GAFFER_TELEMETRY_DEBUG=1` (truthy values: `1`, `true`, `yes`, `on`) and gaffer prints every event as JSON to stderr before sending it. When opted out, no envelopes are constructed and nothing is printed.

## Where data is stored

Telemetry data is stored in PostHog's EU instance. Envelopes transit Cloudflare's edge network on the way there. Cloudflare's standard request logs include IP and are retained for around 30 days; gaffer does not forward IPs to PostHog. The worker that handles ingest is open source and lives in the [gaffer repository](https://github.com/kurrent-io/gaffer/tree/main/telemetry/worker), alongside the [machine-readable schema](https://github.com/kurrent-io/gaffer/tree/main/telemetry/schemas) for the events described above.

## How to delete your data

Email `privacy@kurrent.io` with the identifier gaffer prints below. All events associated with that id are deleted from PostHog within 30 days. Session-stitching and identity-merge rows the worker holds for that id expire automatically within 25 hours and 30 days respectively.

To find your id:

- `gaffer config telemetry status` prints it while you are opted in.
- `gaffer config telemetry off` prints it one last time before clearing local state.

The same identifier is what every gaffer telemetry envelope carries on the wire and what PostHog stores as the person id.
