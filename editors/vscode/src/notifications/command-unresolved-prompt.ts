import * as vscode from "vscode";
import { log } from "../output.js";
import { createStatusBarPrompt } from "./status-prompt.js";

// Surfaced when `gaffer manifest` fails with ENOENT AND the user has
// set a custom `gaffer.command`. Reinstalling via the install prompt
// wouldn't help (npm install -g won't fix a typo in the user's
// configured argv), so this prompt routes to settings instead. Same
// status bar surface as the install/update prompts.

const COMMAND_OPEN = "gaffer._cliCommand.open";

const BUTTON_OPEN_SETTINGS = "Open settings";
const BUTTON_RESET = "Reset to default";

export interface CommandUnresolvedPromptDeps {
	configured: string[];
}

const prompt = createStatusBarPrompt({
	commandId: COMMAND_OPEN,
	onClick: runChoice,
});

// activeDeps captures the click handler's working set since
// registerCommand takes a parameterless callback. Re-assigned on
// every show, so a click that fires after a mid-prompt reassignment
// resolves against the freshest deps (e.g. the user changed
// gaffer.command from typo1 to typo2 while the quickpick was open).
let activeDeps: CommandUnresolvedPromptDeps | null = null;

export function showCommandUnresolvedPrompt(
	deps: CommandUnresolvedPromptDeps,
): void {
	activeDeps = deps;
	prompt.show({
		text: "$(error) gaffer.command unresolved",
		tooltip: `gaffer.command=${JSON.stringify(deps.configured)} not found. Click to fix.`,
		backgroundColor: new vscode.ThemeColor("statusBarItem.errorBackground"),
	});
}

export function dismissCommandUnresolvedPrompt(): void {
	prompt.dismiss();
	activeDeps = null;
}

async function runChoice(): Promise<void> {
	const deps = activeDeps;
	if (!deps) return;
	try {
		const choice = await vscode.window.showQuickPick(
			[
				{
					label: BUTTON_OPEN_SETTINGS,
					description: `Edit gaffer.command (currently ${JSON.stringify(deps.configured)})`,
				},
				{
					label: BUTTON_RESET,
					description: 'Restore gaffer.command to its default of ["gaffer"]',
				},
			],
			{ placeHolder: "gaffer.command points at a binary not on PATH" },
		);
		if (!choice) return;

		if (choice.label === BUTTON_OPEN_SETTINGS) {
			// Don't dismiss - the user might cancel out of settings
			// without fixing the value. The next successful manifest
			// fetch dismisses (see handleManifestOutcome).
			await vscode.commands.executeCommand(
				"workbench.action.openSettings",
				"gaffer.command",
			);
			return;
		}
		if (choice.label === BUTTON_RESET) {
			await vscode.workspace
				.getConfiguration("gaffer")
				.update("command", undefined, vscode.ConfigurationTarget.Global);
			dismissCommandUnresolvedPrompt();
		}
	} catch (err) {
		// Same fire-and-forget posture as the sibling prompts.
		log(
			`command-unresolved prompt failed: ${err instanceof Error ? err.message : String(err)}`,
		);
	}
}

export function __resetCommandUnresolvedPromptStateForTests(): void {
	prompt.__resetForTests();
	activeDeps = null;
}
