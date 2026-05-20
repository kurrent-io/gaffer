import * as vscode from "vscode";
import { log } from "../output.js";

// Surfaced when the initial manifest fetch fails with ENOENT and on
// any subsequent reload that hits the same classification. Offers an
// npm install bootstrap and a docs link. Dismiss persists on
// workspaceState so the user isn't nagged on every activation in the
// same workspace; cleared automatically on the next successful
// manifest fetch so a future uninstall still triggers the prompt.

const DISMISSED_KEY = "gaffer.cliMissingNotificationDismissed";

const NPM_PACKAGE = "@kurrent/gaffer";

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

export async function showCliNotFoundPrompt(
	deps: InstallPromptDeps,
): Promise<void> {
	const choice = await vscode.window.showWarningMessage(
		"gaffer CLI not found on PATH. Install globally with npm?",
		BUTTON_INSTALL,
		BUTTON_DOCS,
		BUTTON_DISMISS,
	);
	if (choice === BUTTON_INSTALL) {
		// Swallowed so a rejecting runInstall can't bubble out of this
		// void-fired prompt as an unhandled rejection. The user sees
		// the install failure in the terminal; we just decline to
		// retry the manifest.
		let ok = false;
		try {
			({ ok } = await deps.runInstall());
		} catch (err) {
			log(
				`install bootstrap failed: ${err instanceof Error ? err.message : String(err)}`,
			);
		}
		if (ok) await deps.onInstalled();
		return;
	}
	if (choice === BUTTON_DOCS) {
		await vscode.env.openExternal(vscode.Uri.parse(INSTALL_DOCS_URL));
		return;
	}
	if (choice === BUTTON_DISMISS) {
		await deps.context.workspaceState.update(DISMISSED_KEY, true);
	}
}

// Spawns a VS Code terminal with npm as its shell so install progress
// (including any auth prompts or EACCES output) is visible live.
// Resolves once the terminal closes; ok=true only when exit code is 0.
export function runNpmInstall(): Promise<{ ok: boolean }> {
	return new Promise((resolve) => {
		let terminal: vscode.Terminal | null = null;
		// Subscribe before createTerminal so a synchronous close (e.g.
		// shellPath not on PATH) can't fire before the listener attaches.
		const sub = vscode.window.onDidCloseTerminal((closed) => {
			if (closed !== terminal) return;
			sub.dispose();
			const code = closed.exitStatus?.code ?? 1;
			log(`npm install -g ${NPM_PACKAGE} exited with code ${code}`);
			resolve({ ok: code === 0 });
		});
		// npm.cmd on Windows: VS Code's createTerminal shellPath
		// doesn't auto-resolve the .cmd shim that ships with the Node
		// installer.
		const shellPath = process.platform === "win32" ? "npm.cmd" : "npm";
		terminal = vscode.window.createTerminal({
			name: TERMINAL_NAME,
			shellPath,
			shellArgs: ["install", "-g", NPM_PACKAGE],
		});
		terminal.show();
	});
}
