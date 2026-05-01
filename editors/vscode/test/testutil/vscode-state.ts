// Test-only handles into the vscode mock's mutable state.
//
// Imported only from tests. Production code never sees this module
// because it imports from "vscode" (mapped to test/__mocks__/vscode.ts
// via vite.config alias).

import type * as vscode from "vscode";
import {
	__clearShownMessages,
	__getShownMessages,
	__resetState,
	state,
} from "../__mocks__/vscode.js";

export function resetVscode(): void {
	__resetState();
	__clearShownMessages();
}

export function setTrusted(trusted: boolean): void {
	state.isTrusted = trusted;
}

// Each call to vscode.workspace.findFiles() shifts one batch off the
// queue. Push as many batches as the test will trigger.
export function queueFindFiles(uris: vscode.Uri[]): void {
	state.findFilesQueue.push(uris);
}

export function setConfiguration(
	section: string,
	key: string,
	options: { value?: unknown; globalValue?: unknown; defaultValue?: unknown },
): void {
	let sec = state.configurations.get(section);
	if (!sec) {
		sec = new Map();
		state.configurations.set(section, sec);
	}
	const inspect: { defaultValue?: unknown; globalValue?: unknown } = {};
	if ("globalValue" in options) inspect.globalValue = options.globalValue;
	if ("defaultValue" in options) inspect.defaultValue = options.defaultValue;
	const entry: { value?: unknown; inspect?: typeof inspect } = {};
	if ("value" in options) entry.value = options.value;
	if (Object.keys(inspect).length > 0) entry.inspect = inspect;
	sec.set(key, entry);
}

// Queue a return value for a *future* showQuickPick call. Push one item
// per expected call, in order.
export function queueQuickPick(value: unknown): void {
	state.quickPickResolutions.push(value);
}

// Queue a return value for a *future* showErrorMessage / showWarningMessage
// / showInformationMessage call.
export function queueMessageResponse(value: unknown): void {
	state.messageResolutions.push(value);
}

// Register a custom handler for executeCommand(name, ...). Used to
// simulate built-in VS Code commands that the extension calls
// (setContext, workbench.action.openSettings, etc.).
export function setCommandHandler(
	name: string,
	handler: (...args: unknown[]) => unknown,
): void {
	state.commandHandlers.set(name, handler);
}

export function setStartDebuggingResult(result: boolean): void {
	state.startDebuggingResult = result;
}

export function fireDebugStarted(session: vscode.DebugSession): void {
	state.debugStarted.fire(session);
}

export function fireDebugTerminated(session: vscode.DebugSession): void {
	state.debugTerminated.fire(session);
}

export function fireDebugCustomEvent(e: vscode.DebugSessionCustomEvent): void {
	state.debugCustomEvent.fire(e);
}

export function fireConfigurationChange(affected: string[]): void {
	state.configurationChanged.fire({
		affectsConfiguration: (s) => affected.includes(s),
	});
}

export function fireWorkspaceTrustGranted(): void {
	// Real grant flips the flag before listeners fire; mirror that so
	// handlers that re-check `vscode.workspace.isTrusted` see `true`.
	state.isTrusted = true;
	state.workspaceTrustGranted.fire();
}

export function setWorkspaceFolders(folders: vscode.WorkspaceFolder[]): void {
	state.workspaceFolders = folders;
}

export const getState = (): typeof state => state;
export const getShownMessages = __getShownMessages;

// The DebugSession the mock auto-fired from the most recent
// startDebugging call. Use this to drive onDidTerminateDebugSession in
// tests where the controller's internal session reference matters
// (terminate identity check).
export function getLastStartedDebugSession(): vscode.DebugSession | null {
	return state.lastStartedDebugSession;
}
