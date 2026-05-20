import * as vscode from "vscode";

export const showNoWorkspace = (): Thenable<unknown> =>
	vscode.window.showWarningMessage(
		"Open a folder first to use KurrentDB Projections.",
	);

// Fired when a command was invoked against a URI that isn't part of
// any workspace folder (e.g. a folder added via "Reveal in Explorer"
// from outside the workspace). Distinct from showNoWorkspace -
// there IS a workspace open, the target just isn't in it.
export const showTargetOutsideWorkspace = (): Thenable<unknown> =>
	vscode.window.showWarningMessage(
		"That folder isn't part of an open workspace. Add it to the workspace first.",
	);

// Brief info toast after scaffold's silent auto-init succeeds. Gives
// the user a visible trace of the side effect (gaffer.toml,
// .gitignore, .gaffer/) without blocking.
export const showAutoInitDone = (folderName: string): Thenable<unknown> =>
	vscode.window.showInformationMessage(
		`Initialized gaffer project in ${folderName}.`,
	);

// gaffer.toml is already present in the target folder. Offer to open
// the existing file rather than running init and surfacing the CLI's
// "already exists" error. Returns true if the user picked Open existing.
export const showTomlExists = (folderName: string): Thenable<boolean> =>
	vscode.window
		.showWarningMessage(
			`gaffer.toml already exists in ${folderName}.`,
			"Open existing",
		)
		.then((choice) => choice === "Open existing");

export const showNoProjections = (): Thenable<unknown> =>
	vscode.window.showInformationMessage(
		"No projections found in this workspace.",
	);
