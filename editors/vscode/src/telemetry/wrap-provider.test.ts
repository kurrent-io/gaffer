import type { Event } from "@kurrent/gaffer-telemetry";
import * as vscode from "vscode";
import { describe, expect, it } from "vitest";

import type { Telemetry } from "./facade.js";
import {
	wrapCodeActionProvider,
	wrapCodeLensProvider,
	wrapMcpServerDefinitionProvider,
	wrapTreeDataProvider,
	wrapWebviewViewProvider,
} from "./wrap-provider.js";

function makeTelemetry(): { telemetry: Telemetry; emitted: Event[] } {
	const emitted: Event[] = [];
	const telemetry: Telemetry = {
		emit: () => {},
		drain: async () => {},
		refreshOptOut: async () => {},
		invokerId: () => null,
		reportException: (phase, err) => {
			emitted.push({
				name: "exception",
				timestamp: "test",
				properties: {
					phase,
					exceptions: [
						{
							type: err instanceof Error ? err.name : "Error",
							value: err instanceof Error ? err.message : String(err),
							in_app: true,
							stacktrace: { type: "raw", frames: [] },
						},
					],
				},
			} as Event);
		},
	};
	return { telemetry, emitted };
}

function expectExceptionEmitted(emitted: Event[], message: string): void {
	expect(emitted).toHaveLength(1);
	const ev = emitted[0];
	if (ev?.name !== "exception") throw new Error("expected exception event");
	expect(ev.properties.phase).toBe("event_processing");
	expect(ev.properties.exceptions[0]?.value).toBe(message);
}

describe("wrapTreeDataProvider", () => {
	it("forwards getChildren and getTreeItem on the happy path", async () => {
		const { telemetry, emitted } = makeTelemetry();
		const inner: vscode.TreeDataProvider<string> = {
			getTreeItem: (s) => new vscode.TreeItem(s),
			getChildren: () => ["a", "b"],
		};
		const wrapped = wrapTreeDataProvider(inner, telemetry);
		expect(await wrapped.getChildren()).toEqual(["a", "b"]);
		expect((await wrapped.getTreeItem("x")).label).toBe("x");
		expect(emitted).toEqual([]);
	});

	it("emits an exception when getChildren throws and re-throws", async () => {
		const { telemetry, emitted } = makeTelemetry();
		const wrapped = wrapTreeDataProvider<string>(
			{
				getTreeItem: (s) => new vscode.TreeItem(s),
				getChildren: () => {
					throw new Error("getChildren boom");
				},
			},
			telemetry,
		);
		await expect(wrapped.getChildren()).rejects.toThrow("getChildren boom");
		expectExceptionEmitted(emitted, "getChildren boom");
	});

	it("preserves onDidChangeTreeData passthrough", () => {
		const { telemetry } = makeTelemetry();
		const emitter = new vscode.EventEmitter<string | undefined>();
		const wrapped = wrapTreeDataProvider<string>(
			{
				onDidChangeTreeData: emitter.event,
				getTreeItem: (s) => new vscode.TreeItem(s),
				getChildren: () => [],
			},
			telemetry,
		);
		expect(wrapped.onDidChangeTreeData).toBe(emitter.event);
	});

	it("wraps optional getParent and resolveTreeItem when present", async () => {
		const { telemetry, emitted } = makeTelemetry();
		const wrapped = wrapTreeDataProvider<string>(
			{
				getTreeItem: (s) => new vscode.TreeItem(s),
				getChildren: () => [],
				getParent: () => {
					throw new Error("getParent boom");
				},
				resolveTreeItem: () => {
					throw new Error("resolveTreeItem boom");
				},
			},
			telemetry,
		);
		await expect(wrapped.getParent?.("x")).rejects.toThrow("getParent boom");
		await expect(
			wrapped.resolveTreeItem?.(
				new vscode.TreeItem("x"),
				"x",
				{} as vscode.CancellationToken,
			),
		).rejects.toThrow("resolveTreeItem boom");
		expect(emitted).toHaveLength(2);
	});

	it("omits optional methods when the inner provider doesn't supply them", () => {
		const { telemetry } = makeTelemetry();
		const wrapped = wrapTreeDataProvider<string>(
			{
				getTreeItem: (s) => new vscode.TreeItem(s),
				getChildren: () => [],
			},
			telemetry,
		);
		expect(wrapped.getParent).toBeUndefined();
		expect(wrapped.resolveTreeItem).toBeUndefined();
	});
});

describe("wrapWebviewViewProvider", () => {
	it("emits an exception when resolveWebviewView throws", async () => {
		const { telemetry, emitted } = makeTelemetry();
		const wrapped = wrapWebviewViewProvider(
			{
				resolveWebviewView: () => {
					throw new Error("resolve boom");
				},
			},
			telemetry,
		);
		await expect(
			wrapped.resolveWebviewView(
				{} as vscode.WebviewView,
				{} as vscode.WebviewViewResolveContext,
				{} as vscode.CancellationToken,
			),
		).rejects.toThrow("resolve boom");
		expectExceptionEmitted(emitted, "resolve boom");
	});
});

describe("wrapCodeLensProvider", () => {
	it("emits an exception when provideCodeLenses throws", async () => {
		const { telemetry, emitted } = makeTelemetry();
		const wrapped = wrapCodeLensProvider(
			{
				provideCodeLenses: () => {
					throw new Error("lens boom");
				},
			},
			telemetry,
		);
		await expect(
			wrapped.provideCodeLenses(
				{} as vscode.TextDocument,
				{} as vscode.CancellationToken,
			),
		).rejects.toThrow("lens boom");
		expectExceptionEmitted(emitted, "lens boom");
	});

	it("preserves onDidChangeCodeLenses passthrough", () => {
		const { telemetry } = makeTelemetry();
		const emitter = new vscode.EventEmitter<void>();
		const wrapped = wrapCodeLensProvider(
			{
				onDidChangeCodeLenses: emitter.event,
				provideCodeLenses: () => [],
			},
			telemetry,
		);
		expect(wrapped.onDidChangeCodeLenses).toBe(emitter.event);
	});

	it("wraps optional resolveCodeLens when present", async () => {
		const { telemetry, emitted } = makeTelemetry();
		const wrapped = wrapCodeLensProvider(
			{
				provideCodeLenses: () => [],
				resolveCodeLens: () => {
					throw new Error("resolveCodeLens boom");
				},
			},
			telemetry,
		);
		await expect(
			wrapped.resolveCodeLens?.(
				{} as vscode.CodeLens,
				{} as vscode.CancellationToken,
			),
		).rejects.toThrow("resolveCodeLens boom");
		expect(emitted).toHaveLength(1);
	});
});

describe("wrapCodeActionProvider", () => {
	it("emits an exception when provideCodeActions throws", async () => {
		const { telemetry, emitted } = makeTelemetry();
		const wrapped = wrapCodeActionProvider(
			{
				provideCodeActions: () => {
					throw new Error("action boom");
				},
			},
			telemetry,
		);
		await expect(
			wrapped.provideCodeActions(
				{} as vscode.TextDocument,
				{} as vscode.Range,
				{} as vscode.CodeActionContext,
				{} as vscode.CancellationToken,
			),
		).rejects.toThrow("action boom");
		expectExceptionEmitted(emitted, "action boom");
	});

	it("wraps optional resolveCodeAction when present", async () => {
		const { telemetry, emitted } = makeTelemetry();
		const wrapped = wrapCodeActionProvider(
			{
				provideCodeActions: () => [],
				resolveCodeAction: () => {
					throw new Error("resolveCodeAction boom");
				},
			},
			telemetry,
		);
		await expect(
			wrapped.resolveCodeAction?.(
				{} as vscode.CodeAction,
				{} as vscode.CancellationToken,
			),
		).rejects.toThrow("resolveCodeAction boom");
		expect(emitted).toHaveLength(1);
	});
});

describe("wrapMcpServerDefinitionProvider", () => {
	it("emits an exception when provideMcpServerDefinitions throws", async () => {
		const { telemetry, emitted } = makeTelemetry();
		const wrapped =
			wrapMcpServerDefinitionProvider<vscode.McpStdioServerDefinition>(
				{
					provideMcpServerDefinitions: () => {
						throw new Error("mcp boom");
					},
				},
				telemetry,
			);
		await expect(
			wrapped.provideMcpServerDefinitions({} as vscode.CancellationToken),
		).rejects.toThrow("mcp boom");
		expectExceptionEmitted(emitted, "mcp boom");
	});

	it("preserves onDidChangeMcpServerDefinitions passthrough", () => {
		const { telemetry } = makeTelemetry();
		const emitter = new vscode.EventEmitter<void>();
		const wrapped =
			wrapMcpServerDefinitionProvider<vscode.McpStdioServerDefinition>(
				{
					onDidChangeMcpServerDefinitions: emitter.event,
					provideMcpServerDefinitions: () => [],
				},
				telemetry,
			);
		expect(wrapped.onDidChangeMcpServerDefinitions).toBe(emitter.event);
	});

	it("omits onDidChangeMcpServerDefinitions when the inner provider doesn't set it", () => {
		const { telemetry } = makeTelemetry();
		const wrapped =
			wrapMcpServerDefinitionProvider<vscode.McpStdioServerDefinition>(
				{ provideMcpServerDefinitions: () => [] },
				telemetry,
			);
		expect(wrapped.onDidChangeMcpServerDefinitions).toBeUndefined();
	});

	it("wraps optional resolveMcpServerDefinition when present", async () => {
		const { telemetry, emitted } = makeTelemetry();
		const wrapped =
			wrapMcpServerDefinitionProvider<vscode.McpStdioServerDefinition>(
				{
					provideMcpServerDefinitions: () => [],
					resolveMcpServerDefinition: () => {
						throw new Error("resolveMcp boom");
					},
				},
				telemetry,
			);
		await expect(
			wrapped.resolveMcpServerDefinition?.(
				{} as vscode.McpStdioServerDefinition,
				{} as vscode.CancellationToken,
			),
		).rejects.toThrow("resolveMcp boom");
		expect(emitted).toHaveLength(1);
	});

	it("omits optional resolveMcpServerDefinition when the inner provider doesn't supply it", () => {
		const { telemetry } = makeTelemetry();
		const wrapped =
			wrapMcpServerDefinitionProvider<vscode.McpStdioServerDefinition>(
				{ provideMcpServerDefinitions: () => [] },
				telemetry,
			);
		expect(wrapped.resolveMcpServerDefinition).toBeUndefined();
	});
});
