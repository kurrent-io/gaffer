# Usage telemetry

The gaffer VS Code extension collects anonymous usage statistics and sends them to Kurrent, Inc. when the extension runs. Telemetry data helps us refine and improve gaffer based on real usage patterns.

The extension also spawns the gaffer CLI as a child process for several of its features (LSP, debug sessions, MCP). The CLI emits its own telemetry under its own rules - see [the gaffer CLI's telemetry notice](https://github.com/kurrent-io/gaffer/tree/main/cli/TELEMETRY.md) for what it collects. When telemetry is on, the extension passes its `emitter_id` to those spawned CLIs so the extension and its child processes are recognised as one user. Opting out of telemetry in the extension also suppresses telemetry in CLI processes the extension spawns. CLI invocations made directly from a terminal follow the CLI's own opt-outs.

## What is usage telemetry

Gaffer telemetry only tracks non-Personally-Identifiable Information. Collected data does not allow Kurrent to fingerprint users by any of the collected data points.

Examples of the telemetry data collected:

- Extension version, host OS and architecture, editor version
- Whether the gaffer CLI is reachable when the extension activates
- Crashes in the extension's own code (gaffer-authored error messages with scrubbed stack frames)

What the extension **does not** track:

- Projection source code, in any form
- Stream names, event names, category names, result-stream patterns
- Function names, variable names, or any identifier from your code
- Error messages produced by your projection code
- File paths beyond basenames in scrubbed stack frames
- KurrentDB connection strings, hostnames, or credentials
- Workspace or folder paths, names, or contents
- Environment variable names or values
- User account or OS user information
- IP addresses

There are two event types. Each is wrapped in an envelope alongside shared install metadata (extension version, host OS, architecture, the runtime environment - `local` or `ci`) before being sent.

The receiving worker stamps each event with its own deploy timestamp.

The precise wire format lives in [`telemetry/schemas/events.cue`](https://github.com/kurrent-io/gaffer/tree/main/telemetry/schemas/events.cue) (event shapes) and [`telemetry/schemas/wire.cue`](https://github.com/kurrent-io/gaffer/tree/main/telemetry/schemas/wire.cue) (envelope).

### `extension_activated`

Records whether the gaffer CLI binary is reachable on the `PATH` when the extension activates, along with the editor version, the CLI version when reachable, and a bucketed activation duration. When the CLI is unreachable, the event carries a categorical reason for the failure: `binary_not_found`, `binary_spawn_failed`, `timeout`, `workspace_untrusted`, or `unknown_error`. No paths or error messages from the failed spawn are attached.

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

Records crashes in the extension's own code (unhandled JS errors in the extension host). Exception messages are always written by gaffer and never propagated from your projection code. Stack frames are scrubbed: file basenames only, user-JS frames dropped entirely.

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
			"name": "exception",
			"timestamp": "2026-05-08T12:34:56.000Z",
			"properties": {
				"exceptions": [
					{
						"type": "Error",
						"value": "failed to start LSP client",
						"in_app": true,
						"stacktrace": {
							"type": "raw",
							"frames": [
								{
									"filename": "client.ts",
									"function": "spawnLanguageClient",
									"lineno": 123,
									"in_app": true
								}
							]
						}
					}
				],
				"phase": "activation"
			}
		}
	]
}
```

</details>

## Disclosure

On first activation, the extension shows a notification similar to:

> Gaffer collects anonymous usage data to improve your experience. The data is collected by Kurrent, Inc. [Learn more]
>
> `[Dismiss]` `[Learn more]` `[Disable telemetry]`

Closing the notification with the X is treated the same as `[Dismiss]` (telemetry stays on). `[Learn more]` opens this page. The notification re-appears on next activation until you pick `[Dismiss]` or `[Disable telemetry]`.

If telemetry collection has already been disabled (for example via `KURRENTDB_TELEMETRY_OPTOUT` carried over from KurrentDB, or `DO_NOT_TRACK`), no disclosure is shown.

## Reporting frequency

The extension emits events at the boundary of work, not on a periodic schedule:

- `extension_activated` is sent once when the extension activates.
- `exception` is sent when the extension's own code crashes.

An extension session that does no work beyond activation emits one event. There is no periodic heartbeat.

## How to opt out

Telemetry transmission can be disabled by any one of the following:

- Pick `[Disable telemetry]` from the first-run notification, or toggle it later from the extension's settings.
- Set VS Code's `telemetry.telemetryLevel` to anything other than `all`. The gaffer extension respects this setting.
- Set `GAFFER_TELEMETRY_OPTOUT`, `KURRENTDB_TELEMETRY_OPTOUT`, or `DO_NOT_TRACK` to a truthy value (`1`, `true`, `yes`, `on`) in the environment VS Code launches under.

When opted out, the extension does not collect telemetry. No envelope is constructed and no event is recorded locally. The opt-out also propagates to CLI processes the extension spawns (LSP, `gaffer dev`, MCP), so those processes do not emit telemetry either. Opting out mid-session takes effect for future spawns. CLI processes already running exit naturally; their own opt-out is checked when they next start.

## How to see what's being sent

Set `GAFFER_TELEMETRY_DEBUG=1` (truthy values: `1`, `true`, `yes`, `on`) in the environment VS Code launches under, and the extension prints every event as JSON to the Gaffer output channel before sending it. When opted out, no envelopes are constructed and nothing is printed.

## Where data is stored

Telemetry data is stored in PostHog's EU instance. Envelopes transit Cloudflare's edge network on the way there. Cloudflare's standard request logs include IP and are retained for around 30 days; gaffer does not forward IPs to PostHog. The worker that handles ingest is open source and lives in the [gaffer repository](https://github.com/kurrent-io/gaffer/tree/main/telemetry/worker), alongside the [machine-readable schema](https://github.com/kurrent-io/gaffer/tree/main/telemetry/schemas) for the events described above.

## How to delete your data

Email `privacy@kurrent.io` with the identifier the extension stores below. All events associated with that id are deleted from PostHog within 30 days. Session-stitching and identity-merge rows the worker holds for that id expire automatically within 25 hours and 30 days respectively.

The extension persists its telemetry id in `telemetry.json` inside VS Code's global storage directory for the gaffer extension (`kurrent-io.gaffer-vscode`). The full path depends on your OS and editor variant:

- Linux: `~/.config/Code/User/globalStorage/kurrent-io.gaffer-vscode/telemetry.json`
- macOS: `~/Library/Application Support/Code/User/globalStorage/kurrent-io.gaffer-vscode/telemetry.json`
- Windows: `%APPDATA%\Code\User\globalStorage\kurrent-io.gaffer-vscode\telemetry.json`

Replace `Code` with `Code - Insiders`, `Cursor`, etc. for other editor variants. The `telemetry_id` field is your id, identical to what every envelope carries as `emitter_id` and what PostHog stores as the person id.
