// Orchestration tests for activate(). Verifies the registrations the
// extension makes on activation and the side-effects of config /
// trust changes. Per-command UX flows live with the command modules.

import * as vscode from "vscode";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { activate } from "./extension.js";
import { LspCodeLensProvider } from "./lsp/lens-provider.js";
import { flushAllMicrotasks } from "../test/testutil/promise.js";
import { makeContext } from "../test/testutil/fake-context.js";
import {
	fireConfigurationChange,
	fireTextDocumentChange,
	fireWorkspaceTrustGranted,
	getState,
	queueFindFiles,
	setTrusted,
	setWorkspaceFolders,
} from "../test/testutil/vscode-state.js";
import { clearLspRequestHandlers } from "../test/__mocks__/vscode-languageclient-node.js";

afterEach(() => {
	// Clear LSP request handlers between tests so a stub
	// installed by one test doesn't leak into the next.
	clearLspRequestHandlers();
});

// Shared test setup: an untrusted workspace where activate's initial
// tryFetchManifest returns null silently (no execFile) and findFiles
// returns []. Tests that need a trusted workspace flip it after
// activate.
async function activateBare(): Promise<vscode.ExtensionContext> {
	setTrusted(false);
	queueFindFiles([]);
	const ctx = makeContext();
	await activate(ctx);
	return ctx;
}

beforeEach(() => {
	vi.restoreAllMocks();
});

describe("activate registrations", () => {
	it("registers the step and state tree data providers and the status webview provider", async () => {
		await activateBare();
		const ids = getState().registeredTreeProviders.map((p) => p.id);
		expect(ids).toEqual(["gaffer.step", "gaffer.state"]);
		const webviewIds = getState().registeredWebviewProviders.map((p) => p.id);
		expect(webviewIds).toEqual(["gaffer.status"]);
	});

	it("registers a debug adapter descriptor factory for `gaffer`", async () => {
		await activateBare();
		const types = getState().registeredDebugFactories.map((f) => f.type);
		expect(types).toEqual(["gaffer"]);
	});

	it("registers a single code lens provider for both **/gaffer.toml and javascript", async () => {
		// Both the toml lens and the entry-script .js lens come
		// from the same LSP server via the same LspCodeLensProvider;
		// one registration with an array selector.
		await activateBare();
		const selectors = getState().registeredCodeLensProviders.map(
			(r) => r.selector,
		);
		expect(selectors).toEqual([
			[
				{ scheme: "file", pattern: "**/gaffer.toml" },
				{ scheme: "file", language: "javascript" },
			],
		]);
	});

	it("registers the lifecycle commands", async () => {
		await activateBare();
		const names = [...getState().registeredCommands.keys()].sort();
		expect(names).toEqual([
			"gaffer.debugProjection",
			"gaffer.debugProjectionPick",
			"gaffer.dismissDiagnostic",
			"gaffer.noop",
			"gaffer.stopDebug",
		]);
	});

	it("registers the gaffer MCP server definition provider", async () => {
		await activateBare();
		const ids = getState().mcpProviders.map((p) => p.id);
		expect(ids).toEqual(["gaffer"]);
	});

	it("refreshes the MCP provider on workspace trust grant", async () => {
		await activateBare();
		const provider = getState().mcpProviders[0]?.provider;
		if (!provider) throw new Error("no MCP provider registered");
		const listener = vi.fn();
		provider.onDidChangeMcpServerDefinitions?.(listener);
		fireWorkspaceTrustGranted();
		expect(listener).toHaveBeenCalled();
	});

	it("refreshes the MCP provider on workspace folder changes", async () => {
		await activateBare();
		const provider = getState().mcpProviders[0]?.provider;
		if (!provider) throw new Error("no MCP provider registered");
		const listener = vi.fn();
		provider.onDidChangeMcpServerDefinitions?.(listener);
		getState().workspaceFoldersChanged.fire({ added: [], removed: [] });
		expect(listener).toHaveBeenCalled();
	});

	it("refreshes the MCP provider when gaffer.command changes", async () => {
		await activateBare();
		const provider = getState().mcpProviders[0]?.provider;
		if (!provider) throw new Error("no MCP provider registered");
		const listener = vi.fn();
		provider.onDidChangeMcpServerDefinitions?.(listener);
		fireConfigurationChange(["gaffer.command"]);
		expect(listener).toHaveBeenCalled();
	});

	it("MCP definitions carry --invoked-via=mcp_provider once trusted + id minted", async () => {
		// activateBare keeps trust=false (MCP returns []); flip trust on
		// + ensure the facade has minted an invoker id, then ask the
		// provider for its definitions.
		await activateBare();
		setTrusted(true);
		setWorkspaceFolders([
			{
				uri: vscode.Uri.file("/ws/a"),
				name: "a",
				index: 0,
			} as vscode.WorkspaceFolder,
		]);
		const provider = getState().mcpProviders[0]?.provider;
		if (!provider) throw new Error("no MCP provider registered");
		const defs = await Promise.resolve(
			provider.provideMcpServerDefinitions({} as vscode.CancellationToken),
		);
		const def = (defs as vscode.McpStdioServerDefinition[])[0];
		if (!def) throw new Error("expected one definition");
		// Position-sensitive: invoker-id and invoked-by always pair, and
		// invoked-via sits adjacent in the linkage trio.
		expect(def.args.slice(0, 3)).toEqual([
			expect.stringMatching(/^--invoker-id=[0-9a-f-]{36}$/),
			"--invoked-by=vscode",
			"--invoked-via=mcp_provider",
		]);
		expect(def.args[3]).toBe("mcp");
	});
});

describe("createDebugAdapterDescriptor", () => {
	function getFactory(): vscode.DebugAdapterDescriptorFactory {
		const f = getState().registeredDebugFactories[0]?.factory;
		if (!f) throw new Error("no debug factory registered");
		return f as vscode.DebugAdapterDescriptorFactory;
	}

	function fakeSession(
		configuration: Record<string, unknown>,
	): vscode.DebugSession {
		return {
			configuration,
		} as unknown as vscode.DebugSession;
	}

	it("returns a DebugAdapterServer when configuration.port is a number", async () => {
		await activateBare();
		const desc = getFactory().createDebugAdapterDescriptor(
			fakeSession({ port: 4711 }),
			// eslint-disable-next-line @typescript-eslint/no-explicit-any
			null as any,
		);
		expect(desc).toBeInstanceOf(vscode.DebugAdapterServer);
		expect((desc as vscode.DebugAdapterServer).port).toBe(4711);
	});

	it("throws when configuration.port is missing", async () => {
		await activateBare();
		expect(() =>
			getFactory().createDebugAdapterDescriptor(
				fakeSession({}),
				// eslint-disable-next-line @typescript-eslint/no-explicit-any
				null as any,
			),
		).toThrow(/missing port/);
	});

	it("throws when configuration.port is non-numeric", async () => {
		await activateBare();
		expect(() =>
			getFactory().createDebugAdapterDescriptor(
				fakeSession({ port: "4711" }),
				// eslint-disable-next-line @typescript-eslint/no-explicit-any
				null as any,
			),
		).toThrow(/missing port/);
	});
});

describe("runtime fatal-error dismissal", () => {
	it("text change clears the runtime diagnostic for that URI", async () => {
		// Stale-on-edit: any change to a file with a runtime error
		// invalidates the squiggle since the in-memory content no
		// longer matches what was running when the error fired.
		const { reportFatalError } = await import("./diagnostics.js");
		await activateBare();
		reportFatalError({
			file: "/p/projection.js",
			line: 1,
			column: 1,
			code: "JS_ERROR",
			description: "boom",
			jsStack: undefined,
			eventId: undefined,
		});
		const coll = getState().diagnosticCollections.find(
			(c) => c.name === "gaffer",
		);
		expect(coll?.entries.has("/p/projection.js")).toBe(true);

		fireTextDocumentChange(vscode.Uri.file("/p/projection.js"));
		expect(coll?.entries.has("/p/projection.js")).toBe(false);
	});

	it("text change leaves diagnostics for other files alone", async () => {
		const { reportFatalError } = await import("./diagnostics.js");
		await activateBare();
		reportFatalError({
			file: "/p/a.js",
			line: 1,
			column: 1,
			code: "JS_ERROR",
			description: "x",
			jsStack: undefined,
			eventId: undefined,
		});
		reportFatalError({
			file: "/p/b.js",
			line: 1,
			column: 1,
			code: "JS_ERROR",
			description: "y",
			jsStack: undefined,
			eventId: undefined,
		});

		fireTextDocumentChange(vscode.Uri.file("/p/a.js"));
		const coll = getState().diagnosticCollections.find(
			(c) => c.name === "gaffer",
		);
		expect(coll?.entries.has("/p/a.js")).toBe(false);
		expect(coll?.entries.has("/p/b.js")).toBe(true);
	});

	it("registers a CodeActionsProvider for any file (matches reportFatalError's URI scope)", async () => {
		// Selector intentionally NOT language-scoped: reportFatalError
		// publishes against any path the runtime emits, so the
		// dismiss-action provider has to cover the same surface.
		await activateBare();
		const providers = getState().registeredCodeActionProviders;
		expect(providers).toHaveLength(1);
		expect(providers[0]?.selector).toMatchObject({ scheme: "file" });
		expect(providers[0]?.selector).not.toHaveProperty("language");
	});

	it("gaffer.dismissDiagnostic command clears the URI's runtime diagnostic", async () => {
		const { reportFatalError } = await import("./diagnostics.js");
		await activateBare();
		reportFatalError({
			file: "/p/projection.js",
			line: 1,
			column: 1,
			code: "JS_ERROR",
			description: "boom",
			jsStack: undefined,
			eventId: undefined,
		});
		const coll = getState().diagnosticCollections.find(
			(c) => c.name === "gaffer",
		);
		expect(coll?.entries.has("/p/projection.js")).toBe(true);

		// Invoke through executeCommand to exercise the registration
		// rather than just the underlying function — catches a future
		// refactor that breaks the command wiring.
		await vscode.commands.executeCommand(
			"gaffer.dismissDiagnostic",
			vscode.Uri.file("/p/projection.js"),
		);
		expect(coll?.entries.has("/p/projection.js")).toBe(false);
	});
});

describe("configuration change filter", () => {
	it("triggers reloadLensState when gaffer.command changes", async () => {
		const setManifest = vi.spyOn(LspCodeLensProvider.prototype, "setManifest");
		await activateBare();
		setManifest.mockClear();

		queueFindFiles([]);
		fireConfigurationChange(["gaffer.command"]);
		await flushAllMicrotasks();
		expect(setManifest).toHaveBeenCalledTimes(1);
	});

	it("does NOT trigger reloadLensState for unrelated config changes", async () => {
		const setManifest = vi.spyOn(LspCodeLensProvider.prototype, "setManifest");
		await activateBare();
		setManifest.mockClear();

		fireConfigurationChange(["editor.fontSize"]);
		await flushAllMicrotasks();
		expect(setManifest).not.toHaveBeenCalled();
	});
});

describe("workspace trust grant", () => {
	// fireWorkspaceTrustGranted flips isTrusted=true before firing.
	// The reload's tryFetchManifest then runs execFile which resolves
	// on a future event-loop tick (not a microtask), so a microtask
	// flush isn't enough - poll until the spy fires.
	async function waitForCall(
		spy: ReturnType<typeof vi.spyOn>,
		ms = 1000,
	): Promise<void> {
		const deadline = Date.now() + ms;
		while (Date.now() < deadline) {
			if (spy.mock.calls.length > 0) return;
			await new Promise<void>((r) => setTimeout(r, 10));
		}
		throw new Error(`spy did not fire within ${ms}ms`);
	}

	it("triggers reloadLensState when trust is granted", async () => {
		const setManifest = vi.spyOn(LspCodeLensProvider.prototype, "setManifest");
		await activateBare();
		setManifest.mockClear();

		queueFindFiles([]);
		fireWorkspaceTrustGranted();
		await waitForCall(setManifest);
		// Trust grant fires both startLanguageClient's deferred-
		// spawn listener and the reloadManifest listener; the spawn
		// path doesn't call setManifest itself but the reload does.
		// We pin "fired at least once" rather than an exact count
		// since refreshChain ordering can vary across runs.
		expect(setManifest.mock.calls.length).toBeGreaterThanOrEqual(1);
	});
});
