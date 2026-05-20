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

// Module-level dedupe so back-to-back ENOENT outcomes (e.g. multiple
// config-change reloads while the toast is open) don't stack
// concurrent prompts. Second caller piggybacks on the in-flight
// promise; cleared in `finally` so the next ENOENT after the user
// acts can prompt again.
let activePrompt: Promise<void> | null = null;

export function showCliNotFoundPrompt(deps: InstallPromptDeps): Promise<void> {
	if (activePrompt !== null) return activePrompt;
	activePrompt = runPrompt(deps).finally(() => {
		activePrompt = null;
	});
	return activePrompt;
}

async function runPrompt(deps: InstallPromptDeps): Promise<void> {
	try {
		const choice = await vscode.window.showWarningMessage(
			"gaffer CLI not found on PATH. Install globally with npm?",
			BUTTON_INSTALL,
			BUTTON_DOCS,
			BUTTON_DISMISS,
		);
		if (choice === BUTTON_INSTALL) {
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
	} catch (err) {
		// The prompt is fire-and-forget from the activation path. Catch
		// here so a rejecting Thenable (openExternal, workspaceState
		// update, onInstalled, or showWarningMessage) can't surface as
		// an unhandled rejection in the extension host.
		log(
			`install prompt failed: ${err instanceof Error ? err.message : String(err)}`,
		);
	}
}

// Spawns a VS Code terminal with npm as its shell so install progress
// (including any auth prompts or EACCES output) is visible live.
// Resolves once the terminal closes; ok=true only when exit code is 0.
export function runNpmInstall(): Promise<{ ok: boolean }> {
	return new Promise((resolve) => {
		let terminal: vscode.Terminal | null = null;
		let done = false;
		const finish = (code: number): void => {
			if (done) return;
			done = true;
			sub.dispose();
			log(`npm install -g ${NPM_PACKAGE} exited with code ${code}`);
			resolve({ ok: code === 0 });
		};
		// Subscribe before createTerminal so a close fired before the
		// assignment below isn't lost from missing listener. If the
		// event arrives while `terminal` is still null (re-entrant
		// close), the identity filter drops it - the post-create
		// exitStatus check below picks up the dropped case.
		const sub = vscode.window.onDidCloseTerminal((closed) => {
			if (closed !== terminal) return;
			finish(closed.exitStatus?.code ?? 1);
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
		// Belt-and-braces: if VS Code fired the close synchronously
		// inside createTerminal, our listener saw it while `terminal`
		// was still null and dropped it. exitStatus is populated by
		// then, so we replay the resolution here.
		if (terminal.exitStatus !== undefined) {
			finish(terminal.exitStatus.code ?? 1);
			return;
		}
		terminal.show();
	});
}
