import * as vscode from "vscode";
import { log } from "../output.js";

export const NPM_PACKAGE = "@kurrent/gaffer";

export interface NpmTerminalOptions {
	name: string;
	args: string[];
}

// Runs npm in a VS Code terminal so install/upgrade progress
// (including auth prompts, EACCES output) is visible live. Resolves
// once the terminal closes; ok=true only when the exit code is 0.
export function runNpmTerminal(
	opts: NpmTerminalOptions,
): Promise<{ ok: boolean }> {
	return new Promise((resolve) => {
		let terminal: vscode.Terminal | null = null;
		let done = false;
		const finish = (code: number): void => {
			if (done) return;
			done = true;
			sub.dispose();
			log(`npm ${opts.args.join(" ")} exited with code ${code}`);
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
			name: opts.name,
			shellPath,
			shellArgs: opts.args,
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
