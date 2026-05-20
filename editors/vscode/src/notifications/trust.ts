import * as vscode from "vscode";

export const showTrustWarning = (): Thenable<unknown> =>
	vscode.window
		.showWarningMessage(
			"Trust this workspace to use KurrentDB Projections commands.",
			"Manage Trust",
		)
		.then((choice) => {
			if (choice === "Manage Trust") {
				void vscode.commands.executeCommand("workbench.trust.manage");
			}
		});
