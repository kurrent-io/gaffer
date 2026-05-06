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

// Shared shape for any "LSP isn't running" surface: error toast
// with a "View Output" button that opens the LSP channel (NOT
// our generic Gaffer channel - the LSP one carries the
// actionable detail). Used by both the initial-start failure
// path (c.start() threw) and the give-up-after-restarts path.
const showLspBroken = (
	message: string,
	channel: vscode.OutputChannel,
): Thenable<unknown> =>
	vscode.window.showErrorMessage(message, "View Output").then((choice) => {
		if (choice === "View Output") {
			channel.show(true);
		}
	});

export const showLspFailedToStart = (
	detail: string,
	channel: vscode.OutputChannel,
): Thenable<unknown> =>
	showLspBroken(`Gaffer LSP failed to start: ${detail}`, channel);

export const showLspCrashed = (
	channel: vscode.OutputChannel,
): Thenable<unknown> =>
	showLspBroken(
		"Gaffer LSP keeps crashing - features that depend on it (lenses, projection list) are unavailable.",
		channel,
	);

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
