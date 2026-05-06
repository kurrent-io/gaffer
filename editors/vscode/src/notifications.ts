// All user-facing toasts/dialogs in one place. Each function expresses
// the meaning of the notification rather than the primitive
// (showWarningMessage / showErrorMessage / etc.) - call sites read
// `notifications.showTrustWarning()` instead of choosing the right
// vscode.window method and re-typing the message.
//
// All exports return Thenable<unknown> for a uniform contract: callers
// await when they need to sequence on dismissal (e.g. to delay cleanup
// until the user has seen the error), or `void` when they don't care.

import * as vscode from "vscode";
import { showOutputPanel } from "./output.js";

export const showManifestFailure = (err: unknown): Thenable<unknown> => {
	const raw = err instanceof Error ? err.message : String(err);
	const truncated = raw.length > 200 ? `${raw.slice(0, 200)}…` : raw;
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

export const showTrustWarning = (): Thenable<unknown> =>
	vscode.window
		.showWarningMessage(
			"Trust this workspace to enable Gaffer debugging.",
			"Manage Trust",
		)
		.then((choice) => {
			if (choice === "Manage Trust") {
				void vscode.commands.executeCommand("workbench.trust.manage");
			}
		});

export const showNoProjections = (): Thenable<unknown> =>
	vscode.window.showInformationMessage(
		"Gaffer: no projections found in this workspace.",
	);

export const showLspNotReady = (): Thenable<unknown> =>
	vscode.window.showInformationMessage(
		"Gaffer is still starting up. Try again in a moment.",
	);

export const showLspError = (): Thenable<unknown> =>
	vscode.window
		.showErrorMessage(
			"Gaffer: failed to fetch projections from the LSP server.",
			"View Output",
		)
		.then((choice) => {
			if (choice === "View Output") {
				showOutputPanel();
			}
		});

export const showProjectionFault = (
	exitCode: number | null,
): Thenable<unknown> =>
	vscode.window.showErrorMessage(
		`Gaffer: projection faulted (exit code ${exitCode})`,
	);

export const showStartFailure = (message: string): Thenable<unknown> =>
	vscode.window.showErrorMessage(`Gaffer: ${message}`);

export const showProjectionFailed = (): Thenable<unknown> =>
	vscode.window.showErrorMessage(
		"Gaffer: projection failed - see Problems panel",
	);

export const showPortInUse = (description: string): Thenable<unknown> =>
	vscode.window
		.showErrorMessage(
			`Gaffer: ${description}. Change gaffer.debugPort or set it to -1 to let the OS pick a free port.`,
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
