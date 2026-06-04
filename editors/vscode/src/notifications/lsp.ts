import * as vscode from "vscode";
import { showOutputPanel } from "../output.js";

export const showLspNotReady = (): Thenable<unknown> =>
	vscode.window.showInformationMessage(
		"Gaffer is still starting up. Try again in a moment.",
	);

export const showLspError = (): Thenable<unknown> =>
	vscode.window
		.showErrorMessage(
			"Failed to fetch projections from the LSP server.",
			"View Output",
		)
		.then((choice) => {
			if (choice === "View Output") {
				showOutputPanel();
			}
		});

// Shared shape for any "LSP isn't running" surface: error toast
// with a "View Output" button that opens the LSP channel (NOT
// our generic extension channel - the LSP one carries the
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
	showLspBroken(`Language server failed to start: ${detail}`, channel);

export const showLspCrashed = (
	channel: vscode.OutputChannel,
): Thenable<unknown> =>
	showLspBroken(
		"Language server keeps crashing - features that depend on it (lenses, projection list) are unavailable.",
		channel,
	);
