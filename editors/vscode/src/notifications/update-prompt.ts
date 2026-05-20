import * as vscode from "vscode";
import gt from "semver/functions/gt.js";
import { log } from "../output.js";
import { NPM_PACKAGE, runNpmTerminal } from "./npm.js";

// Surfaced when `gaffer manifest` reports an `updateAvailable` newer
// than the running CLI. Offers an npm upgrade bootstrap, a per-version
// skip, and a perma-suppress. The skipped-version key lives on
// globalState rather than workspaceState because the install is
// global: dismissing 1.2.3 in workspace A and being re-prompted in
// workspace B would be the same nag in different clothes. The
// perma-suppress is a user setting (gaffer.cliUpdateNotifications)
// rather than a hidden globalState flag so the user can re-enable
// from the settings UI without us having to ship a separate command.

const DISMISSED_VERSION_KEY = "gaffer.cliUpdate.dismissedVersion";
const NOTIFICATIONS_SETTING = "cliUpdateNotifications";

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

// activePrompt collapses concurrent calls onto one in-flight toast.
// lastPromptedVersion stops a closed-without-acting toast (X / focus
// loss) re-firing on the next manifest reload within the same
// session: the prompt is one-per-session-per-version, and the next
// session (or a newer version) re-prompts.
let activePrompt: Promise<void> | null = null;
let lastPromptedVersion: string | null = null;

export function showCliUpdatePrompt(deps: UpdatePromptDeps): Promise<void> {
	if (activePrompt !== null) return activePrompt;
	if (lastPromptedVersion === deps.latest) return Promise.resolve();
	lastPromptedVersion = deps.latest;
	activePrompt = runPrompt(deps).finally(() => {
		activePrompt = null;
	});
	return activePrompt;
}

// Test-only: reset module state between tests. Module-level guards
// (activePrompt, lastPromptedVersion) would otherwise leak between
// it() blocks since vitest doesn't re-import the module per test.
export function __resetUpdatePromptStateForTests(): void {
	activePrompt = null;
	lastPromptedVersion = null;
}

async function runPrompt(deps: UpdatePromptDeps): Promise<void> {
	try {
		const choice = await vscode.window.showInformationMessage(
			`gaffer ${deps.latest} is available (you have ${deps.current}). Update?`,
			BUTTON_UPDATE,
			BUTTON_SKIP,
			BUTTON_NEVER,
		);
		if (choice === BUTTON_UPDATE) {
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
			} else {
				// Failed click: clear the session guard so the user can
				// retry the same version after fixing whatever broke npm.
				// Skip and Never are deliberate dismissals and keep
				// their own persistent state, so they don't reset here.
				lastPromptedVersion = null;
			}
			return;
		}
		if (choice === BUTTON_SKIP) {
			await deps.context.globalState.update(DISMISSED_VERSION_KEY, deps.latest);
			return;
		}
		if (choice === BUTTON_NEVER) {
			await vscode.workspace
				.getConfiguration("gaffer")
				.update(
					NOTIFICATIONS_SETTING,
					false,
					vscode.ConfigurationTarget.Global,
				);
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
