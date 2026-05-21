import * as vscode from "vscode";
import { log } from "../output.js";
import { NPM_PACKAGE, runNpmTerminal } from "./npm.js";
import { createStatusBarPrompt } from "./status-prompt.js";

// Surfaced when the initial manifest fetch fails with ENOENT and on
// any subsequent reload that hits the same classification. Drops a
// status bar item; click opens a quickpick with Install / Install
// guide / Dismiss. Dismiss persists on workspaceState so the user
// isn't nagged on every activation in the same workspace; cleared
// automatically on the next successful manifest fetch so a future
// uninstall still triggers the prompt.

const DISMISSED_KEY = "gaffer.cliMissingNotificationDismissed";
const COMMAND_OPEN = "gaffer._cliInstall.open";

// Docs root rather than a pinned anchor: the install + nvm/fnm
// section lives under this URL but the exact anchor is in flight.
// UI-1591 tracks pointing every docs link at canonical anchors
// before the extension publishes.
export const INSTALL_DOCS_URL = "https://docs.kurrent.io/gaffer/";

const TERMINAL_NAME = "KurrentDB Projections: Install CLI";

const BUTTON_INSTALL = "Install";
const BUTTON_DOCS = "Install guide";
const BUTTON_DISMISS = "Dismiss";

export interface InstallPromptDeps {
	context: vscode.ExtensionContext;
	runInstall: () => Promise<{ ok: boolean }>;
	onInstalled: () => Promise<void> | void;
}

export function isInstallPromptDismissed(
	context: vscode.ExtensionContext,
): boolean {
	return context.workspaceState.get<boolean>(DISMISSED_KEY) === true;
}

// Cleared whenever a manifest fetch eventually succeeds so the prompt
// isn't permanently suppressed: a future uninstall (or `gaffer.command`
// pointing at a now-missing binary) should re-prompt.
export function clearInstallPromptDismissed(
	context: vscode.ExtensionContext,
): Thenable<void> {
	return context.workspaceState.update(DISMISSED_KEY, undefined);
}

const prompt = createStatusBarPrompt({
	commandId: COMMAND_OPEN,
	onClick: runChoice,
});

// activeDeps captures the click handler's working set since
// registerCommand takes a parameterless callback. Cleared in lockstep
// with prompt.dismiss.
let activeDeps: InstallPromptDeps | null = null;

export function showCliNotFoundPrompt(deps: InstallPromptDeps): void {
	if (isInstallPromptDismissed(deps.context)) return;
	activeDeps = deps;
	prompt.show({
		text: "$(error) gaffer not installed",
		tooltip: "gaffer CLI not found on PATH. Click to install.",
		backgroundColor: new vscode.ThemeColor("statusBarItem.errorBackground"),
	});
}

async function runChoice(): Promise<void> {
	const deps = activeDeps;
	if (!deps) return;
	try {
		const choice = await vscode.window.showQuickPick(
			[
				{
					label: BUTTON_INSTALL,
					description: `Run npm install -g ${NPM_PACKAGE}`,
				},
				{
					label: BUTTON_DOCS,
					description: "Open the gaffer install guide in your browser",
				},
				{
					label: BUTTON_DISMISS,
					description: "Hide this prompt in the current workspace",
				},
			],
			{ placeHolder: "gaffer CLI not found on PATH" },
		);
		if (!choice) return;

		if (choice.label === BUTTON_INSTALL) {
			let ok = false;
			try {
				({ ok } = await deps.runInstall());
			} catch (err) {
				log(
					`install bootstrap failed: ${err instanceof Error ? err.message : String(err)}`,
				);
			}
			if (ok) {
				await deps.onInstalled();
				dismiss();
			}
			// On failure the item stays visible; the user can re-click
			// after fixing whatever broke npm.
			return;
		}
		if (choice.label === BUTTON_DOCS) {
			// Don't dismiss - the user might read the docs and then
			// come back to click Install.
			await vscode.env.openExternal(vscode.Uri.parse(INSTALL_DOCS_URL));
			return;
		}
		if (choice.label === BUTTON_DISMISS) {
			await deps.context.workspaceState.update(DISMISSED_KEY, true);
			dismiss();
		}
	} catch (err) {
		// The prompt is fire-and-forget from the activation path. Catch
		// here so a rejecting Thenable (openExternal, workspaceState
		// update, onInstalled, or showQuickPick) can't surface as
		// an unhandled rejection in the extension host.
		log(
			`install prompt failed: ${err instanceof Error ? err.message : String(err)}`,
		);
	}
}

function dismiss(): void {
	prompt.dismiss();
	activeDeps = null;
}

// Called from handleManifestOutcome when a manifest fetch succeeds
// so the item disappears if the user fixed the install via a
// separate terminal rather than the prompt's own Install button.
export function dismissCliNotFoundPrompt(): void {
	dismiss();
}

export function __resetInstallPromptStateForTests(): void {
	prompt.__resetForTests();
	activeDeps = null;
}

export function runNpmInstall(): Promise<{ ok: boolean }> {
	return runNpmTerminal({
		name: TERMINAL_NAME,
		args: ["install", "-g", NPM_PACKAGE],
	});
}
