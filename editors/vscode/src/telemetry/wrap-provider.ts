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

import { type WrapContext, wrapAsync } from "./wrap.js";

function wrap<A extends unknown[], R>(
	wrapCtx: WrapContext,
	fn: (...args: A) => PromiseLike<R> | R,
): (...args: A) => Promise<R> {
	return wrapAsync(wrapCtx, "event_processing", fn);
}

export function wrapTreeDataProvider<T>(
	p: vscode.TreeDataProvider<T>,
	wrapCtx: WrapContext,
): vscode.TreeDataProvider<T> {
	const out: vscode.TreeDataProvider<T> = {
		getTreeItem: wrap(wrapCtx, p.getTreeItem.bind(p)),
		getChildren: wrap(wrapCtx, p.getChildren.bind(p)),
	};
	if (p.onDidChangeTreeData) out.onDidChangeTreeData = p.onDidChangeTreeData;
	if (p.getParent) out.getParent = wrap(wrapCtx, p.getParent.bind(p));
	if (p.resolveTreeItem) {
		out.resolveTreeItem = wrap(wrapCtx, p.resolveTreeItem.bind(p));
	}
	return out;
}

export function wrapWebviewViewProvider(
	p: vscode.WebviewViewProvider,
	wrapCtx: WrapContext,
): vscode.WebviewViewProvider {
	return {
		resolveWebviewView: wrap(wrapCtx, p.resolveWebviewView.bind(p)),
	};
}

export function wrapCodeLensProvider(
	p: vscode.CodeLensProvider,
	wrapCtx: WrapContext,
): vscode.CodeLensProvider {
	const out: vscode.CodeLensProvider = {
		provideCodeLenses: wrap(wrapCtx, p.provideCodeLenses.bind(p)),
	};
	if (p.onDidChangeCodeLenses)
		out.onDidChangeCodeLenses = p.onDidChangeCodeLenses;
	if (p.resolveCodeLens) {
		out.resolveCodeLens = wrap(wrapCtx, p.resolveCodeLens.bind(p));
	}
	return out;
}

export function wrapCodeActionProvider(
	p: vscode.CodeActionProvider,
	wrapCtx: WrapContext,
): vscode.CodeActionProvider {
	const out: vscode.CodeActionProvider = {
		provideCodeActions: wrap(wrapCtx, p.provideCodeActions.bind(p)),
	};
	if (p.resolveCodeAction) {
		out.resolveCodeAction = wrap(wrapCtx, p.resolveCodeAction.bind(p));
	}
	return out;
}

export function wrapMcpServerDefinitionProvider<
	T extends vscode.McpServerDefinition,
>(
	p: vscode.McpServerDefinitionProvider<T>,
	wrapCtx: WrapContext,
): vscode.McpServerDefinitionProvider<T> {
	const out: vscode.McpServerDefinitionProvider<T> = {
		provideMcpServerDefinitions: wrap(
			wrapCtx,
			p.provideMcpServerDefinitions.bind(p),
		),
		...(p.onDidChangeMcpServerDefinitions !== undefined && {
			onDidChangeMcpServerDefinitions: p.onDidChangeMcpServerDefinitions,
		}),
	};
	if (p.resolveMcpServerDefinition) {
		out.resolveMcpServerDefinition = wrap(
			wrapCtx,
			p.resolveMcpServerDefinition.bind(p),
		);
	}
	return out;
}
