import * as vscode from "vscode";
import gt from "semver/functions/gt.js";
import { log } from "../output.js";
import { NPM_PACKAGE, runNpmTerminal } from "./npm.js";

// Surfaced when `gaffer manifest` reports an `updateAvailable` newer
// than the running CLI. Offers an npm upgrade bootstrap, a per-version
// skip, and a perma-suppress. Both dismissal flags live on
// globalState rather than workspaceState because the install is
// global - dismissing 1.2.3 in workspace A and being re-prompted in
// workspace B would be the same nag in different clothes.

const DISMISSED_VERSION_KEY = "gaffer.cliUpdate.dismissedVersion";
const NEVER_ASK_KEY = "gaffer.cliUpdate.neverAsk";

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

// True when the prompt should NOT fire for `latest`: either the user
// chose Never ask, or they previously skipped an equal-or-newer
// version (semver-compared so dismissed=1.2.3 doesn't suppress 1.2.4).
export function isCliUpdatePromptSuppressed(
	context: vscode.ExtensionContext,
	latest: string,
): boolean {
	if (context.globalState.get<boolean>(NEVER_ASK_KEY) === true) return true;
	const dismissed = context.globalState.get<string>(DISMISSED_VERSION_KEY);
	if (dismissed !== undefined && !isNewerVersion(latest, dismissed)) {
		return true;
	}
	return false;
}

// Module-level dedupe so back-to-back manifest reloads can't stack
// concurrent prompts. Cleared in `finally` so the next manifest
// reporting an update after the user acts can prompt again.
let activePrompt: Promise<void> | null = null;

export function showCliUpdatePrompt(deps: UpdatePromptDeps): Promise<void> {
	if (activePrompt !== null) return activePrompt;
	activePrompt = runPrompt(deps).finally(() => {
		activePrompt = null;
	});
	return activePrompt;
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
			if (ok) await deps.onUpdated();
			return;
		}
		if (choice === BUTTON_SKIP) {
			await deps.context.globalState.update(DISMISSED_VERSION_KEY, deps.latest);
			return;
		}
		if (choice === BUTTON_NEVER) {
			await deps.context.globalState.update(NEVER_ASK_KEY, true);
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

// Wraps node-semver's gt so a malformed manifest version (the CLI
// could ship one some day) can't throw out of the activation path.
// Falls back to "not newer" - the toast suppresses rather than fires
// on bad data.
export function isNewerVersion(latest: string, current: string): boolean {
	try {
		return gt(latest, current);
	} catch {
		return false;
	}
}
