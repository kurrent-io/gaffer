import * as vscode from "vscode";
import gt from "semver/functions/gt.js";
import { log } from "../output.js";
import { NPM_PACKAGE, runNpmTerminal } from "./npm.js";

// Surfaced when `gaffer manifest` reports an `updateAvailable` newer
// than the running CLI. Drops a status bar item rather than a toast
// because VS Code auto-dismisses third-party message toasts after a
// few seconds (sticky is reserved for built-in extensions per the
// upstream source); a status bar item stays put until the user acts.
// Click opens a quickpick with Update / Skip this version / Never
// ask. Skipped-version key on globalState so it follows the user
// across workspaces (the install is global). Never-ask is a user
// setting (gaffer.cliUpdateNotifications) so it's discoverable and
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

// Module-level state: at most one status bar item lives at a time.
// activeDeps captures the click handler's working set since
// registerCommand takes a parameterless callback. lastPromptedVersion
// stops a freshly-dismissed item re-creating on the next manifest
// reload within the same session - the next session (or a newer
// version) re-creates.
let activeItem: vscode.StatusBarItem | null = null;
let activeDisposables: vscode.Disposable[] = [];
let activeDeps: UpdatePromptDeps | null = null;
let lastPromptedVersion: string | null = null;

export function showCliUpdatePrompt(deps: UpdatePromptDeps): void {
	if (activeItem !== null) return;
	if (lastPromptedVersion === deps.latest) return;
	lastPromptedVersion = deps.latest;

	activeDeps = deps;
	const item = vscode.window.createStatusBarItem(
		vscode.StatusBarAlignment.Right,
		100,
	);
	item.text = `$(arrow-circle-up) gaffer ${deps.latest}`;
	item.tooltip = `gaffer ${deps.latest} is available (you have ${deps.current}). Click to update.`;
	item.backgroundColor = new vscode.ThemeColor(
		"statusBarItem.warningBackground",
	);
	item.command = COMMAND_OPEN;
	activeDisposables.push(
		vscode.commands.registerCommand(COMMAND_OPEN, runChoice),
	);
	item.show();
	activeItem = item;
	deps.context.subscriptions.push(item);
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
				dismissActiveItem();
			} else {
				// Failed click: clear the session guard so the user
				// can retry the same version after fixing npm.
				lastPromptedVersion = null;
			}
			return;
		}
		if (choice.label === BUTTON_SKIP) {
			await deps.context.globalState.update(DISMISSED_VERSION_KEY, deps.latest);
			dismissActiveItem();
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
			dismissActiveItem();
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

function dismissActiveItem(): void {
	activeItem?.dispose();
	for (const d of activeDisposables) d.dispose();
	activeItem = null;
	activeDisposables = [];
	activeDeps = null;
}

// Test-only: reset module state between tests. Module-level guards
// (activeItem, lastPromptedVersion, ...) would otherwise leak
// between it() blocks since vitest doesn't re-import the module per
// test.
export function __resetUpdatePromptStateForTests(): void {
	dismissActiveItem();
	lastPromptedVersion = null;
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
