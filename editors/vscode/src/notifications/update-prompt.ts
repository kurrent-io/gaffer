import * as vscode from "vscode";
import gt from "semver/functions/gt.js";
import { log } from "../output.js";
import { NPM_PACKAGE, runNpmTerminal } from "./npm.js";
import { createStatusBarPrompt } from "./status-prompt.js";

// Surfaced when `gaffer manifest` reports an `updateAvailable` newer
// than the running CLI. Drops a status bar item; click opens a
// quickpick with Update / Skip this version / Never ask. Skipped-
// version key on globalState so it follows the user across
// workspaces (the install is global). Never-ask is a user setting
// (gaffer.cliUpdateNotifications) so it's discoverable and
// reversible from the settings UI.

const DISMISSED_VERSION_KEY = "gaffer.cliUpdate.dismissedVersion";
const NOTIFICATIONS_SETTING = "cliUpdateNotifications";
const COMMAND_OPEN = "gaffer._cliUpdate.open";

const TERMINAL_NAME = "KurrentDB Projections: Update CLI";

const BUTTON_UPDATE = "Update";
const BUTTON_SKIP = "Skip this version";
const BUTTON_NEVER = "Never ask";

export interface UpdatePromptDeps {
	context: vscode.ExtensionContext;
	current: string;
	latest: string;
	runUpdate: () => Promise<{ ok: boolean }>;
	onUpdated: () => Promise<void> | void;
}

export function isCliUpdatePromptSuppressed(
	context: vscode.ExtensionContext,
	latest: string,
): boolean {
	if (!notificationsEnabled()) return true;
	const dismissed = context.globalState.get<string>(DISMISSED_VERSION_KEY);
	if (dismissed !== undefined && !isNewerVersion(latest, dismissed)) {
		return true;
	}
	return false;
}

function notificationsEnabled(): boolean {
	return vscode.workspace
		.getConfiguration("gaffer")
		.get<boolean>(NOTIFICATIONS_SETTING, true);
}

const prompt = createStatusBarPrompt({
	commandId: COMMAND_OPEN,
	onClick: runChoice,
});

// activeDeps captures the click handler's working set since
// registerCommand takes a parameterless callback. Cleared in lockstep
// with prompt.dismiss.
let activeDeps: UpdatePromptDeps | null = null;

export function showCliUpdatePrompt(deps: UpdatePromptDeps): void {
	activeDeps = deps;
	prompt.show({
		text: `$(arrow-circle-up) gaffer ${deps.latest}`,
		tooltip: `gaffer ${deps.latest} is available (you have ${deps.current}). Click to update.`,
		backgroundColor: new vscode.ThemeColor("statusBarItem.warningBackground"),
	});
}

async function runChoice(): Promise<void> {
	const deps = activeDeps;
	if (!deps) return;
	try {
		const choice = await vscode.window.showQuickPick(
			[
				{
					label: BUTTON_UPDATE,
					description: `Run npm install -g ${NPM_PACKAGE}@latest`,
				},
				{
					label: BUTTON_SKIP,
					description: "Suppress until a newer version is available",
				},
				{
					label: BUTTON_NEVER,
					description: "Disable gaffer.cliUpdateNotifications",
				},
			],
			{
				placeHolder: `gaffer ${deps.latest} is available (you have ${deps.current})`,
			},
		);
		if (!choice) return;

		if (choice.label === BUTTON_UPDATE) {
			let ok = false;
			try {
				({ ok } = await deps.runUpdate());
			} catch (err) {
				log(
					`update bootstrap failed: ${err instanceof Error ? err.message : String(err)}`,
				);
			}
			if (ok) {
				await deps.onUpdated();
				dismiss();
			}
			// On failure the item stays visible so the user can retry
			// by clicking it again after fixing whatever broke npm.
			return;
		}
		if (choice.label === BUTTON_SKIP) {
			await deps.context.globalState.update(DISMISSED_VERSION_KEY, deps.latest);
			dismiss();
			return;
		}
		if (choice.label === BUTTON_NEVER) {
			await vscode.workspace
				.getConfiguration("gaffer")
				.update(
					NOTIFICATIONS_SETTING,
					false,
					vscode.ConfigurationTarget.Global,
				);
			dismiss();
		}
	} catch (err) {
		// Same fire-and-forget posture as the install prompt: catch
		// here so a rejecting Thenable can't escape as an unhandled
		// rejection on the extension host.
		log(
			`update prompt failed: ${err instanceof Error ? err.message : String(err)}`,
		);
	}
}

function dismiss(): void {
	prompt.dismiss();
	activeDeps = null;
}

// Called from handleManifestOutcome on outcomes that invalidate
// the cached updateAvailable (manifest error, missing binary, or a
// success where no upgrade is reported). The user-flow dismissals
// (Update / Skip / Never) go through the click handler directly.
export function dismissCliUpdatePrompt(): void {
	dismiss();
}

export function __resetUpdatePromptStateForTests(): void {
	prompt.__resetForTests();
	activeDeps = null;
}

export function runNpmUpdate(): Promise<{ ok: boolean }> {
	return runNpmTerminal({
		name: TERMINAL_NAME,
		args: ["install", "-g", `${NPM_PACKAGE}@latest`],
	});
}

// Wraps semver.gt so a malformed manifest version can't throw out of
// the activation path. Falls back to "not newer" on bad input.
export function isNewerVersion(latest: string, current: string): boolean {
	try {
		return gt(latest, current);
	} catch {
		return false;
	}
}
