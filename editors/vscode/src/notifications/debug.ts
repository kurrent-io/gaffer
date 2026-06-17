import * as vscode from "vscode";

export const showDebugUnsupported = (): Thenable<unknown> =>
	vscode.window.showErrorMessage(
		"This gaffer version doesn't support `dev --debug`. Update gaffer or set gaffer.command to a newer build.",
	);

export const showProjectionFault = (
	exitCode: number | null,
): Thenable<unknown> =>
	vscode.window.showErrorMessage(`Projection faulted (exit code ${exitCode})`);

export const showStartFailure = (message: string): Thenable<unknown> =>
	vscode.window.showErrorMessage(message);

export const showProjectionFailed = (): Thenable<unknown> =>
	vscode.window.showErrorMessage("Projection failed - see Problems panel");

// A connection/runtime failure that ended the run (e.g. a dropped subscription).
// Surfaces the reason the CLI reported so the user isn't left with a silent
// failure or a bare exit code.
export const showRunError = (description: string): Thenable<unknown> =>
	vscode.window.showErrorMessage(description);

export const showPortInUse = (description: string): Thenable<unknown> =>
	vscode.window
		.showErrorMessage(
			`${description}. Change gaffer.debugPort or set it to -1 to let the OS pick a free port.`,
			"Open Settings",
		)
		.then((choice) => {
			if (choice === "Open Settings") {
				void vscode.commands.executeCommand(
					"workbench.action.openSettings",
					"gaffer.debugPort",
				);
			}
		});
