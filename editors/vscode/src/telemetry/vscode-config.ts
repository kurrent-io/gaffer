// telemetry.telemetryLevel is one of "all" / "error" / "crash" /
// "off" or unset. checkOptOut opts out when the setting is *set to*
// something other than "all"; unset falls through to the next
// cascade signal.

import * as vscode from "vscode";

/**
 * Return `telemetry.telemetryLevel` from VS Code, or undefined if the
 * setting is unset. Pure: just reads configuration, no policy.
 */
export function readVscodeTelemetryLevel(): string | undefined {
	return vscode.workspace
		.getConfiguration("telemetry")
		.get<string>("telemetryLevel");
}
