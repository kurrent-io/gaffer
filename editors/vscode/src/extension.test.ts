// Orchestration tests for activate(). The full happy path of
// runProjection ends in `controller.start()` which spawns a real CLI
// subprocess - that's the e2e tier. The four bail-early paths
// (untrusted, empty index, manifest fetch fails, user cancels) are
// covered here. The `manifest fetch fails` case spawns ENOENT against
// a non-existent path - documented exception, fails before any IO.

import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import * as vscode from "vscode";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { activate } from "./extension.js";
import * as cliModule from "./discovery/cli.js";
import { LspCodeLensProvider } from "./lsp/lens-provider.js";
import { flushAllMicrotasks } from "../test/testutil/promise.js";
import { makeContext } from "../test/testutil/fake-context.js";
import {
	fireConfigurationChange,
	fireTextDocumentChange,
	fireWorkspaceTrustGranted,
	getShownMessages,
	getState,
	getStatusBarItems,
	queueFindFiles,
	queueMessageResponse,
	queueQuickPick,
	setConfiguration,
	setTrusted,
	setWorkspaceFolders,
} from "../test/testutil/vscode-state.js";
import {
	clearLspRequestHandlers,
	setLspRequestHandler,
} from "../test/__mocks__/vscode-languageclient-node.js";

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

// Stub tryFetchManifest with a fake-success so the LSP spawn's
// manifest gate clears in tests without requiring a real `gaffer`
// binary on the test runner's PATH. Returns the spy so tests
// that want to override the response (e.g. simulate a failed
// fetch on reload) can re-use it.
function stubManifestFetch(): ReturnType<typeof vi.spyOn> {
	return vi.spyOn(cliModule, "tryFetchManifest").mockResolvedValue({
		version: "test",
		commands: { dev: { flags: ["debug"] } },
	});
}

// Poll until getLanguageClient() returns a live client. The LSP
// spawn is now gated on a successful manifest fetch (which runs
// execFile and resolves on a future event-loop tick rather than a
// microtask), so flushAllMicrotasks alone isn't enough after a
// trust grant - we need to wait for the spawn to land.
async function waitForLspClient(ms = 1000): Promise<void> {
	const { getLanguageClient } = await import("./lsp/client.js");
	const deadline = Date.now() + ms;
	while (Date.now() < deadline) {
		if (getLanguageClient()) return;
		await new Promise<void>((r) => setTimeout(r, 10));
	}
	throw new Error(`LSP client did not spawn within ${ms}ms`);
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
			"gaffer.init",
			"gaffer.noop",
			"gaffer.runProjection",
			"gaffer.scaffold",
			"gaffer.scaffoldHere",
			"gaffer.signIn",
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

describe("runProjection bail-early paths", () => {
	function runProjection(): Promise<unknown> {
		const handler = getState().registeredCommands.get("gaffer.runProjection");
		if (!handler) throw new Error("gaffer.runProjection not registered");
		return Promise.resolve(handler() as unknown);
	}

	it("untrusted workspace: shows trust warning, no quickpick", async () => {
		await activateBare();
		// Workspace stays untrusted from activateBare.
		await runProjection();
		const msgs = getShownMessages();
		expect(msgs.some((m) => /Trust this workspace/.test(m.message))).toBe(true);
		expect(getState().quickPickCalls).toEqual([]);
	});

	it("trusted but no projections: shows 'no projections' info, no quickpick", async () => {
		await activateBare();
		// Flip trust + fire the grant event so the trust-gated LSP
		// spawn proceeds. activateBare leaves the workspace
		// untrusted, which now defers the spawn. The spawn is also
		// gated on a successful manifest fetch, so stub a fake
		// success and wait for the client to come up before
		// exercising fetchProjections.
		stubManifestFetch();
		setTrusted(true);
		fireWorkspaceTrustGranted();
		await waitForLspClient();
		// Stub workspace/symbol with an empty array to differentiate
		// "LSP returned no projections" from "LSP not ready yet".
		setLspRequestHandler("workspace/symbol", () => []);
		await runProjection();
		const msgs = getShownMessages();
		expect(msgs.some((m) => /No projections found/.test(m.message))).toBe(true);
		expect(getState().quickPickCalls).toEqual([]);
	});

	it("LSP not ready: shows 'still starting' info, no quickpick", async () => {
		// Distinct from "no projections": when getLanguageClient
		// returns undefined (e.g. activate's spawn hasn't finished
		// initialize, or trust was just granted), runProjection
		// should tell the user to retry, not claim the workspace
		// has no projections.
		await activateBare();
		setTrusted(true);
		// Override the module-level client to undefined for this
		// test by NOT installing a workspace/symbol handler AND
		// stopping the client - but here, the client mock IS
		// "ready" since activate spawned it. Easiest path: rebuild
		// the test using the production client-state via stop.
		const { stopLanguageClient } = await import("./lsp/client.js");
		await stopLanguageClient();
		await runProjection();
		const msgs = getShownMessages();
		expect(msgs.some((m) => /still starting/.test(m.message))).toBe(true);
		expect(getState().quickPickCalls).toEqual([]);
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

	it("gaffer.signIn opens a gaffer auth terminal for the env in the config's directory", async () => {
		await activateBare();
		setTrusted(true); // sign-in is trust-gated in the command handler
		await vscode.commands.executeCommand("gaffer.signIn", {
			env: "prod",
			tomlUri: vscode.Uri.file("/ws/gaffer.toml"),
		});
		const term = getState().terminals.find((t) =>
			t.name.includes("gaffer auth"),
		);
		expect(term?.name).toBe("gaffer auth (prod)");
		expect(term?.options.shellArgs).toEqual(
			expect.arrayContaining(["auth", "--env", "prod"]),
		);
		expect(term?.options.cwd).toBe(
			vscode.Uri.joinPath(vscode.Uri.file("/ws/gaffer.toml"), "..").fsPath,
		);
		expect(term?.showCount).toBeGreaterThan(0);
	});

	it("gaffer.signIn is a no-op in an untrusted workspace", async () => {
		await activateBare(); // leaves the workspace untrusted
		await vscode.commands.executeCommand("gaffer.signIn", {
			env: "prod",
			tomlUri: vscode.Uri.file("/ws/gaffer.toml"),
		});
		expect(
			getState().terminals.some((t) => t.name.includes("gaffer auth")),
		).toBe(false);
	});
});

describe("manifest outcome routing", () => {
	const DISMISSED_KEY = "gaffer.cliMissingNotificationDismissed";

	function stubManifestEnoent(): void {
		vi.spyOn(cliModule, "tryFetchManifest").mockImplementation(
			async (_cwd, _telemetry, onError) => {
				const err: NodeJS.ErrnoException = new Error("spawn ENOENT");
				err.code = "ENOENT";
				onError?.(err);
				return null;
			},
		);
	}

	it("ENOENT on activation shows the install prompt (not the generic toast)", async () => {
		stubManifestEnoent();
		setTrusted(true);
		queueFindFiles([]);
		await activate(makeContext());
		// Install prompt is now a status bar item, not a toast.
		const installItem = getStatusBarItems().find((i) =>
			i.text.includes("gaffer not installed"),
		);
		const genericToast = getShownMessages().find((m) =>
			/Gaffer CLI failed/.test(m.message),
		);
		expect(installItem).toBeDefined();
		expect(installItem?.disposed).toBe(false);
		expect(genericToast).toBeUndefined();
	});

	it("ENOENT on activation respects a prior workspace dismissal", async () => {
		stubManifestEnoent();
		setTrusted(true);
		queueFindFiles([]);
		const ctx = makeContext();
		await ctx.workspaceState.update(DISMISSED_KEY, true);
		await activate(ctx);
		const msgs = getShownMessages();
		expect(
			getStatusBarItems().find((i) => i.text.includes("gaffer not installed")),
		).toBeUndefined();
		expect(
			msgs.find((m) => /Gaffer CLI failed/.test(m.message)),
		).toBeUndefined();
		// Dismissal isn't cleared by a still-failing fetch.
		expect(ctx.workspaceState.get(DISMISSED_KEY)).toBe(true);
	});

	it("ENOENT with gaffer.command explicitly set to its default shows the install prompt", async () => {
		// User explicitly set the setting to ["gaffer"] (same as the
		// contributed default). Reset to default wouldn't change
		// anything, so the unresolved prompt is the wrong recovery -
		// route to install instead.
		setConfiguration("gaffer", "command", {
			value: ["gaffer"],
			globalValue: ["gaffer"],
			defaultValue: ["gaffer"],
		});
		stubManifestEnoent();
		setTrusted(true);
		queueFindFiles([]);
		await activate(makeContext());
		const items = getStatusBarItems();
		expect(
			items.find((i) => i.text.includes("gaffer not installed")),
		).toBeDefined();
		expect(
			items.find((i) => i.text.includes("gaffer.command unresolved")),
		).toBeUndefined();
	});

	it("ENOENT with customised gaffer.command shows the unresolved prompt, not the install prompt", async () => {
		// User typo'd or otherwise pointed gaffer.command at a missing
		// binary. Reinstalling via npm wouldn't help, so we surface
		// the unresolved-command status bar item instead.
		setConfiguration("gaffer", "command", {
			value: ["gwaffer"],
			globalValue: ["gwaffer"],
		});
		stubManifestEnoent();
		setTrusted(true);
		queueFindFiles([]);
		await activate(makeContext());
		const items = getStatusBarItems();
		expect(
			items.find((i) => i.text.includes("gaffer.command unresolved")),
		).toBeDefined();
		expect(
			items.find((i) => i.text.includes("gaffer not installed")),
		).toBeUndefined();
	});

	it("non-ENOENT activation failure shows the generic toast", async () => {
		vi.spyOn(cliModule, "tryFetchManifest").mockImplementation(
			async (_cwd, _telemetry, onError) => {
				const err: NodeJS.ErrnoException = new Error("permission denied");
				err.code = "EACCES";
				onError?.(err);
				return null;
			},
		);
		setTrusted(true);
		queueFindFiles([]);
		await activate(makeContext());
		const msgs = getShownMessages();
		expect(
			msgs.find((m) => /gaffer CLI not found/i.test(m.message)),
		).toBeUndefined();
		expect(msgs.find((m) => /Gaffer CLI failed/.test(m.message))).toBeDefined();
	});

	it("successful initial manifest clears a prior dismissal", async () => {
		stubManifestFetch();
		setTrusted(true);
		queueFindFiles([]);
		const ctx = makeContext();
		await ctx.workspaceState.update(DISMISSED_KEY, true);
		await activate(ctx);
		expect(ctx.workspaceState.get(DISMISSED_KEY)).toBeUndefined();
	});

	it("LSP spawns when workspace is already trusted at activate", async () => {
		// Regression: handleManifestOutcome populates latestManifest
		// after startLanguageClient has already evaluated its gate
		// predicate. Without retryStartLanguageClient after the
		// initial outcome, the deferred spawn never re-fires on the
		// already-trusted-at-activate happy path because the trust-
		// grant listener only triggers on a transition.
		stubManifestFetch();
		setTrusted(true);
		queueFindFiles([]);
		await activate(makeContext());
		await waitForLspClient();
	});

	it("a subsequent failed reload clears a prior update prompt", async () => {
		// First activate succeeds with updateAvailable set, so the
		// update prompt fires. Then a config-change reload fails with
		// ENOENT - the update prompt must dismiss because the cached
		// updateAvailable info doesn't apply to the new (broken) state.
		vi.spyOn(cliModule, "tryFetchManifest").mockResolvedValueOnce({
			version: "0.1.0",
			updateAvailable: "0.2.0",
			commands: { dev: { flags: ["debug"] } },
		});
		setTrusted(true);
		queueFindFiles([]);
		await activate(makeContext());
		expect(
			getStatusBarItems().find((i) => i.text.includes("gaffer 0.2.0")),
		).toBeDefined();

		// Now flip the stub to an ENOENT and reload.
		vi.mocked(cliModule.tryFetchManifest).mockImplementation(
			async (_cwd, _telemetry, onError) => {
				const err: NodeJS.ErrnoException = new Error("ENOENT");
				err.code = "ENOENT";
				onError?.(err);
				return null;
			},
		);
		fireConfigurationChange(["gaffer.command"]);
		await flushAllMicrotasks();

		const updateItem = getStatusBarItems().find((i) =>
			i.text.includes("gaffer 0.2.0"),
		);
		expect(updateItem?.disposed).toBe(true);
	});

	it("a subsequent successful reload clears a prior unresolved prompt", async () => {
		// First activate fails with ENOENT and a customised
		// gaffer.command -> unresolved prompt visible. Then the user
		// fixes the setting in a separate terminal; the next reload
		// succeeds. The unresolved prompt must dismiss.
		setConfiguration("gaffer", "command", {
			value: ["gwaffer"],
			globalValue: ["gwaffer"],
		});
		stubManifestEnoent();
		setTrusted(true);
		queueFindFiles([]);
		await activate(makeContext());
		expect(
			getStatusBarItems().find((i) =>
				i.text.includes("gaffer.command unresolved"),
			),
		).toBeDefined();

		vi.mocked(cliModule.tryFetchManifest).mockResolvedValue({
			version: "test",
			commands: { dev: { flags: ["debug"] } },
		});
		fireConfigurationChange(["gaffer.command"]);
		await flushAllMicrotasks();

		const unresolvedItem = getStatusBarItems().find((i) =>
			i.text.includes("gaffer.command unresolved"),
		);
		expect(unresolvedItem?.disposed).toBe(true);
	});

	it("a subsequent successful reload clears a prior install prompt", async () => {
		// First activate fails with ENOENT -> install prompt visible.
		// Then a config-change reload succeeds. Install prompt must
		// dismiss even though no Install button was clicked (e.g. the
		// user fixed things in a separate terminal).
		stubManifestEnoent();
		setTrusted(true);
		queueFindFiles([]);
		await activate(makeContext());
		expect(
			getStatusBarItems().find((i) => i.text.includes("gaffer not installed")),
		).toBeDefined();

		vi.mocked(cliModule.tryFetchManifest).mockResolvedValue({
			version: "test",
			commands: { dev: { flags: ["debug"] } },
		});
		fireConfigurationChange(["gaffer.command"]);
		await flushAllMicrotasks();

		const installItem = getStatusBarItems().find((i) =>
			i.text.includes("gaffer not installed"),
		);
		expect(installItem?.disposed).toBe(true);
	});

	it("successful reload clears a prior dismissal", async () => {
		// First activate with ENOENT + a pre-set dismissal so the flag
		// survives the initial outcome; then flip the stub to a success
		// and fire a config-change reload. The reload's success path
		// must clear the flag.
		stubManifestEnoent();
		setTrusted(true);
		queueFindFiles([]);
		const ctx = makeContext();
		await ctx.workspaceState.update(DISMISSED_KEY, true);
		await activate(ctx);
		expect(ctx.workspaceState.get(DISMISSED_KEY)).toBe(true);
		vi.mocked(cliModule.tryFetchManifest).mockResolvedValue({
			version: "test",
			commands: { dev: { flags: ["debug"] } },
		});
		fireConfigurationChange(["gaffer.command"]);
		await flushAllMicrotasks();
		expect(ctx.workspaceState.get(DISMISSED_KEY)).toBeUndefined();
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

// runProjection's manifest-fetch-fails and pick-canceled paths need a
// non-empty projection index, which means writing a real toml. Easier
// to do this in fs-backed tests below.

// runProjection paths that need a non-empty index. Backed by a real
// tmp toml since createProjectIndex reads via node:fs. The
// manifest-fetch-fails case spawns execFile against a non-existent
// path - documented exception to the "no spawn in extension.test.ts"
// header. This is an ENOENT path; the spawn fails before any IO.
describe("runProjection (with a populated projection list)", () => {
	let tmpDir: string;

	beforeEach(() => {
		tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "gaffer-ext-"));
		clearLspRequestHandlers();
	});

	afterEach(() => {
		fs.rmSync(tmpDir, { recursive: true, force: true });
		clearLspRequestHandlers();
	});

	function runProjection(): Promise<unknown> {
		const handler = getState().registeredCommands.get("gaffer.runProjection");
		if (!handler) throw new Error("gaffer.runProjection not registered");
		return Promise.resolve(handler() as unknown);
	}

	function stubProjectionsResponse(name: string, tomlPath: string): void {
		setLspRequestHandler("workspace/symbol", () => [
			{
				name,
				kind: 12,
				location: {
					uri: vscode.Uri.file(tomlPath).toString(),
					range: {
						start: { line: 0, character: 0 },
						end: { line: 0, character: 14 },
					},
				},
			},
		]);
	}

	it("user cancels the quickpick: silent no-op, controller.start not invoked", async () => {
		stubManifestFetch();
		await activateBare();
		setTrusted(true);
		fireWorkspaceTrustGranted();
		await waitForLspClient();
		stubProjectionsResponse("checkout", path.join(tmpDir, "gaffer.toml"));
		queueQuickPick(undefined); // user dismisses
		await runProjection();
		// start would call buildArgv -> getConfiguration -> spawn, which
		// would push to startDebuggingCalls. Cancelation must short-circuit.
		expect(getState().startDebuggingCalls).toEqual([]);
	});

	it("manifest fetch fails: shows error toast, controller.start not invoked", async () => {
		// Initial activate + trust grant gets the LSP up. The "fetch
		// fails" half of this test then reverts the stub to the real
		// implementation so the post-pick manifest fetch in
		// runProjection (which uses the bad gaffer.command set below)
		// hits ENOENT and surfaces showManifestFailure as expected.
		const stub = stubManifestFetch();
		await activateBare();
		setTrusted(true);
		fireWorkspaceTrustGranted();
		await waitForLspClient();
		stub.mockRestore();
		stubProjectionsResponse("checkout", path.join(tmpDir, "gaffer.toml"));
		queueQuickPick({
			label: "checkout",
			description: "checkout/gaffer.toml",
			projection: {
				name: "checkout",
				tomlUri: vscode.Uri.file(path.join(tmpDir, "gaffer.toml")),
			},
		});
		// Make tryFetchManifest fail by pointing gaffer.command at a
		// non-existent binary; showManifestFailure fires; runProjection
		// returns before start.
		setConfiguration("gaffer", "command", {
			globalValue: [path.join(tmpDir, "missing-binary")],
		});
		// Drain the (possible) toast click resolution.
		queueMessageResponse(undefined);
		await runProjection();
		const msgs = getShownMessages();
		expect(msgs.some((m) => /Gaffer CLI failed/.test(m.message))).toBe(true);
		expect(getState().startDebuggingCalls).toEqual([]);
	});
});
