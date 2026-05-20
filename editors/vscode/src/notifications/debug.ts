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
