// Wrappers that interpose `wrapAsync` on the methods VS Code calls
// back into on registered providers. A throw inside a provider method
// fires an `exception` envelope (phase `event_processing`) before
// propagating to the VS Code host. Internal code keeps the original
// provider reference; registrations get the wrapped facade.
//
// All methods are wrapped with `wrapAsync`, which converts sync
// returns to Promises. VS Code accepts either via its `ProviderResult`
// shape, so a sync provider becomes async-by-one-microtask through
// the wrapper. Use wrapAsync uniformly so a future change from
// `() => T` to `() => Promise<T>` doesn't silently bypass the catch.

import type * as vscode from "vscode";

import type { Telemetry } from "./facade.js";
import { wrapAsync } from "./wrap.js";

function wrap<A extends unknown[], R>(
	telemetry: Telemetry,
	fn: (...args: A) => PromiseLike<R> | R,
): (...args: A) => Promise<R> {
	return wrapAsync(telemetry, "event_processing", fn);
}

export function wrapTreeDataProvider<T>(
	p: vscode.TreeDataProvider<T>,
	telemetry: Telemetry,
): vscode.TreeDataProvider<T> {
	const out: vscode.TreeDataProvider<T> = {
		getTreeItem: wrap(telemetry, p.getTreeItem.bind(p)),
		getChildren: wrap(telemetry, p.getChildren.bind(p)),
	};
	if (p.onDidChangeTreeData) out.onDidChangeTreeData = p.onDidChangeTreeData;
	if (p.getParent) out.getParent = wrap(telemetry, p.getParent.bind(p));
	if (p.resolveTreeItem) {
		out.resolveTreeItem = wrap(telemetry, p.resolveTreeItem.bind(p));
	}
	return out;
}

export function wrapWebviewViewProvider(
	p: vscode.WebviewViewProvider,
	telemetry: Telemetry,
): vscode.WebviewViewProvider {
	return {
		resolveWebviewView: wrap(telemetry, p.resolveWebviewView.bind(p)),
	};
}

export function wrapCodeLensProvider(
	p: vscode.CodeLensProvider,
	telemetry: Telemetry,
): vscode.CodeLensProvider {
	const out: vscode.CodeLensProvider = {
		provideCodeLenses: wrap(telemetry, p.provideCodeLenses.bind(p)),
	};
	if (p.onDidChangeCodeLenses)
		out.onDidChangeCodeLenses = p.onDidChangeCodeLenses;
	if (p.resolveCodeLens) {
		out.resolveCodeLens = wrap(telemetry, p.resolveCodeLens.bind(p));
	}
	return out;
}

export function wrapCodeActionProvider(
	p: vscode.CodeActionProvider,
	telemetry: Telemetry,
): vscode.CodeActionProvider {
	const out: vscode.CodeActionProvider = {
		provideCodeActions: wrap(telemetry, p.provideCodeActions.bind(p)),
	};
	if (p.resolveCodeAction) {
		out.resolveCodeAction = wrap(telemetry, p.resolveCodeAction.bind(p));
	}
	return out;
}

export function wrapMcpServerDefinitionProvider<
	T extends vscode.McpServerDefinition,
>(
	p: vscode.McpServerDefinitionProvider<T>,
	telemetry: Telemetry,
): vscode.McpServerDefinitionProvider<T> {
	const out: vscode.McpServerDefinitionProvider<T> = {
		provideMcpServerDefinitions: wrap(
			telemetry,
			p.provideMcpServerDefinitions.bind(p),
		),
		...(p.onDidChangeMcpServerDefinitions !== undefined && {
			onDidChangeMcpServerDefinitions: p.onDidChangeMcpServerDefinitions,
		}),
	};
	if (p.resolveMcpServerDefinition) {
		out.resolveMcpServerDefinition = wrap(
			telemetry,
			p.resolveMcpServerDefinition.bind(p),
		);
	}
	return out;
}
