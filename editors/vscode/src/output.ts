// The Gaffer output channel and its primitive write/show operations.
//
// Module-level singleton: VS Code only ever has one Gaffer output channel
// for the lifetime of the extension, and treating it as ambient
// infrastructure (rather than a value passed through every constructor)
// matches that reality. Init must happen once at activation; until then
// the writes are silently no-ops (with console mirror for `log`).

import * as vscode from "vscode";

let channel: vscode.OutputChannel | null = null;

export function initOutput(context: vscode.ExtensionContext): void {
	channel = vscode.window.createOutputChannel("Gaffer", "log");
	context.subscriptions.push(channel);
}

// Diagnostic log: writes to the channel and mirrors to console for
// extension-host debugging.
export const log = (msg: string): void => {
	channel?.appendLine(msg);
	console.log(`Gaffer: ${msg}`);
};

// Append a streamed line to the channel. Used by renderCliMessage to
// print structured projection output - no console mirror, since that
// stream is for the user, not for debugging the extension.
export const writeOutput = (line: string): void => {
	channel?.appendLine(line);
};

export const clearOutput = (): void => {
	channel?.clear();
};

export const showOutputPanel = (): void => {
	channel?.show();
};

// Test-only: drop the cached channel so the next initOutput in a fresh
// test starts clean. Production never calls this.
export const __resetForTest = (): void => {
	channel = null;
};
