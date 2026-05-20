import * as vscode from "vscode";
import { showOutputPanel } from "../output.js";

export const showManifestFailure = (err: unknown): Thenable<unknown> => {
	const raw = err instanceof Error ? err.message : String(err);
	// execFileAsync stashes stderr on err.cause.stderr (kept off
	// err.message so telemetry never accidentally ships local paths).
	const cause =
		err instanceof Error && typeof err.cause === "object" ? err.cause : null;
	const stderr =
		cause !== null && typeof (cause as { stderr?: unknown }).stderr === "string"
			? (cause as { stderr: string }).stderr
			: "";
	const detail = stderr ? `${raw} (stderr: ${stderr})` : raw;
	const truncated = detail.length > 200 ? `${detail.slice(0, 200)}…` : detail;
	return vscode.window
		.showErrorMessage(
			`Gaffer CLI failed: ${truncated}`,
			"View Output",
			"Open Settings",
		)
		.then((choice) => {
			if (choice === "View Output") {
				showOutputPanel();
			} else if (choice === "Open Settings") {
				void vscode.commands.executeCommand(
					"workbench.action.openSettings",
					"gaffer.command",
				);
			}
		});
};

// Surface for a `gaffer <command>` spawn failure. Mirrors
// showManifestFailure's stderr-on-cause handling so the user sees the
// CLI's actual complaint, not the wrapper "execFile failed" message.
export const showCliCommandFailure = (
	command: string,
	err: unknown,
): Thenable<unknown> => {
	const raw = err instanceof Error ? err.message : String(err);
	const cause =
		err instanceof Error && typeof err.cause === "object" ? err.cause : null;
	const stderr =
		cause !== null && typeof (cause as { stderr?: unknown }).stderr === "string"
			? (cause as { stderr: string }).stderr
			: "";
	const detail = stderr || raw;
	const truncated = detail.length > 200 ? `${detail.slice(0, 200)}…` : detail;
	return vscode.window
		.showErrorMessage(`gaffer ${command} failed: ${truncated}`, "View Output")
		.then((choice) => {
			if (choice === "View Output") {
				showOutputPanel();
			}
		});
};
