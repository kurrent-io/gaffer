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
import { JsCodeLensProvider } from "./lensing/js-provider.js";
import { LspCodeLensProvider } from "./lsp/lens-provider.js";
import { flushAllMicrotasks } from "../test/testutil/promise.js";
import { makeContext } from "../test/testutil/fake-context.js";
import {
	fireConfigurationChange,
	fireTextDocumentChange,
	fireWorkspaceTrustGranted,
	getShownMessages,
	getState,
	queueFindFiles,
	queueFindFilesGate,
	queueMessageResponse,
	queueQuickPick,
	setConfiguration,
	setTrusted,
} from "../test/testutil/vscode-state.js";

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

	it("registers code lens providers for **/gaffer.toml and javascript", async () => {
		await activateBare();
		const selectors = getState().registeredCodeLensProviders.map(
			(r) => r.selector,
		);
		expect(selectors).toEqual([
			{ scheme: "file", pattern: "**/gaffer.toml" },
			{ scheme: "file", language: "javascript" },
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
			"gaffer.runProjection",
			"gaffer.stopDebug",
		]);
	});

	it("creates a file watcher on **/gaffer.toml", async () => {
		await activateBare();
		expect(getState().fileWatchers.map((w) => w.pattern)).toEqual([
			"**/gaffer.toml",
		]);
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
		setTrusted(true);
		// runProjection calls createProjectIndex -> findFiles (empty queue
		// returns []), so the index is empty.
		await runProjection();
		const msgs = getShownMessages();
		expect(msgs.some((m) => /no projections found/.test(m.message))).toBe(true);
		expect(getState().quickPickCalls).toEqual([]);
	});
});

describe("tomlWatcher reload chain", () => {
	it("setIndex+setManifest fires on the lens providers when a toml change is observed", async () => {
		const tomlSetIndex = vi.spyOn(JsCodeLensProvider.prototype, "setIndex");
		const lspSetManifest = vi.spyOn(
			LspCodeLensProvider.prototype,
			"setManifest",
		);
		const jsSetManifest = vi.spyOn(JsCodeLensProvider.prototype, "setManifest");
		await activateBare();
		// Reset call history accumulated during activate's own initial load.
		tomlSetIndex.mockClear();
		lspSetManifest.mockClear();
		jsSetManifest.mockClear();

		queueFindFiles([]); // for the reload's createProjectIndex
		const watcher = getState().fileWatchers[0];
		watcher?.emitChange(vscode.Uri.file("/p/gaffer.toml"));
		await flushAllMicrotasks();
		expect(tomlSetIndex).toHaveBeenCalledTimes(1);
		expect(lspSetManifest).toHaveBeenCalledTimes(1);
		expect(jsSetManifest).toHaveBeenCalledTimes(1);
	});

	it("rapid back-to-back changes serialise through refreshChain (one reload at a time)", async () => {
		// Proof of serialisation: gate each findFiles call. With the
		// chain, only the *first* reload's findFiles is in-flight after
		// firing both events; the second is queued behind it. Without
		// the chain, both would be in-flight concurrently.
		const setIndex = vi.spyOn(JsCodeLensProvider.prototype, "setIndex");
		await activateBare();
		setIndex.mockClear();

		queueFindFiles([]);
		queueFindFiles([]);
		const gate1 = queueFindFilesGate();
		const gate2 = queueFindFilesGate();
		const findFilesCallsBefore = getState().findFilesCalls.length;

		const watcher = getState().fileWatchers[0];
		watcher?.emitChange(vscode.Uri.file("/p/gaffer.toml"));
		watcher?.emitChange(vscode.Uri.file("/p/gaffer.toml"));
		await flushAllMicrotasks();
		// Serialised: only the first reload has reached findFiles.
		expect(getState().findFilesCalls.length - findFilesCallsBefore).toBe(1);
		expect(setIndex).toHaveBeenCalledTimes(0);

		// Release the first gate -> first reload completes -> second
		// reload starts and reaches findFiles.
		gate1.release();
		await flushAllMicrotasks();
		expect(setIndex).toHaveBeenCalledTimes(1);
		expect(getState().findFilesCalls.length - findFilesCallsBefore).toBe(2);

		// Release the second gate -> second reload completes.
		gate2.release();
		await flushAllMicrotasks();
		expect(setIndex).toHaveBeenCalledTimes(2);
	});

	it("create event triggers a reload", async () => {
		const setIndex = vi.spyOn(JsCodeLensProvider.prototype, "setIndex");
		await activateBare();
		setIndex.mockClear();

		queueFindFiles([]);
		const watcher = getState().fileWatchers[0];
		watcher?.emitCreate(vscode.Uri.file("/p/gaffer.toml"));
		await flushAllMicrotasks();
		expect(setIndex).toHaveBeenCalledTimes(1);
	});

	it("delete event triggers a reload", async () => {
		const setIndex = vi.spyOn(JsCodeLensProvider.prototype, "setIndex");
		await activateBare();
		setIndex.mockClear();

		queueFindFiles([]);
		const watcher = getState().fileWatchers[0];
		watcher?.emitDelete(vscode.Uri.file("/p/gaffer.toml"));
		await flushAllMicrotasks();
		expect(setIndex).toHaveBeenCalledTimes(1);
	});

	it("the watcher is on context.subscriptions for disposal", async () => {
		const ctx = await activateBare();
		expect(
			ctx.subscriptions.some((d) => d === getState().fileWatchers[0]),
		).toBe(true);
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
		const setIndex = vi.spyOn(JsCodeLensProvider.prototype, "setIndex");
		await activateBare();
		setIndex.mockClear();

		queueFindFiles([]);
		fireConfigurationChange(["gaffer.command"]);
		await flushAllMicrotasks();
		expect(setIndex).toHaveBeenCalledTimes(1);
	});

	it("does NOT trigger reloadLensState for unrelated config changes", async () => {
		const setIndex = vi.spyOn(JsCodeLensProvider.prototype, "setIndex");
		await activateBare();
		setIndex.mockClear();

		fireConfigurationChange(["editor.fontSize"]);
		await flushAllMicrotasks();
		expect(setIndex).not.toHaveBeenCalled();
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
		const setIndex = vi.spyOn(JsCodeLensProvider.prototype, "setIndex");
		await activateBare();
		setIndex.mockClear();

		queueFindFiles([]);
		fireWorkspaceTrustGranted();
		await waitForCall(setIndex);
		expect(setIndex).toHaveBeenCalledTimes(1);
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
describe("runProjection (with a real projection index)", () => {
	let tmpDir: string;

	beforeEach(() => {
		tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "gaffer-ext-"));
		const tomlPath = path.join(tmpDir, "gaffer.toml");
		const entryPath = path.join(tmpDir, "checkout.js");
		fs.writeFileSync(
			tomlPath,
			`[[projection]]\nname = "checkout"\nentry = "checkout.js"\n`,
		);
		fs.writeFileSync(entryPath, "fromAll().when({})\n");
	});

	afterEach(() => {
		fs.rmSync(tmpDir, { recursive: true, force: true });
	});

	function runProjection(): Promise<unknown> {
		const handler = getState().registeredCommands.get("gaffer.runProjection");
		if (!handler) throw new Error("gaffer.runProjection not registered");
		return Promise.resolve(handler() as unknown);
	}

	it("user cancels the quickpick: silent no-op, controller.start not invoked", async () => {
		await activateBare();
		setTrusted(true);
		queueFindFiles([vscode.Uri.file(path.join(tmpDir, "gaffer.toml"))]);
		queueQuickPick(undefined); // user dismisses
		await runProjection();
		// start would call buildArgv -> getConfiguration -> spawn, which
		// would push to startDebuggingCalls. Cancelation must short-circuit.
		expect(getState().startDebuggingCalls).toEqual([]);
	});

	it("manifest fetch fails: shows error toast, controller.start not invoked", async () => {
		await activateBare();
		setTrusted(true);
		queueFindFiles([vscode.Uri.file(path.join(tmpDir, "gaffer.toml"))]);
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
