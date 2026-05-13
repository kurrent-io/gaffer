import { EventEmitter } from "node:events";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
	CloseAction,
	ErrorAction,
	LanguageClient,
	holdLspStart,
	resetLspMock,
} from "../../test/__mocks__/vscode-languageclient-node.js";
import {
	makeErrorHandler,
	getLanguageClient,
	startLanguageClient,
	stopLanguageClient,
} from "./client.js";
import { flushAllMicrotasks } from "../../test/testutil/promise.js";
import { makeContext } from "../../test/testutil/fake-context.js";
import {
	getShownMessages,
	resetVscode,
	setTrusted,
} from "../../test/testutil/vscode-state.js";

// Stub child_process.spawn so the LSP factory doesn't try to fork a
// real `gaffer` binary in tests. Returns a no-op EventEmitter shaped
// like ChildProcess; tests inspect `spawn.mock.calls` for argv.
const spawnMock = vi.hoisted(() => vi.fn());
vi.mock("node:child_process", () => ({ spawn: spawnMock }));

afterEach(() => {
	resetVscode();
});

// Stub OutputChannel - the handler only calls .show() inside the
// "View Output" toast click path, which we don't exercise here.
const stubChannel = {
	name: "stub",
	append: () => {},
	appendLine: () => {},
	clear: () => {},
	hide: () => {},
	replace: () => {},
	show: () => {},
	dispose: () => {},
} as unknown as Parameters<typeof makeErrorHandler>[0];

describe("startLanguageClient invokerId wiring", () => {
	afterEach(async () => {
		await stopLanguageClient();
		resetVscode();
		resetLspMock();
		spawnMock.mockReset();
	});

	function fakeChild(): EventEmitter {
		return new EventEmitter();
	}

	async function runFactory(): Promise<void> {
		const c = getLanguageClient() as LanguageClient | undefined;
		if (!c) throw new Error("expected a LanguageClient");
		const factory = c.serverOptions as () => Promise<unknown>;
		await factory();
	}

	it("invokes spawn with --invoker-id when getInvokerId returns an id", async () => {
		setTrusted(true);
		spawnMock.mockImplementation(() => fakeChild());
		startLanguageClient(
			makeContext(),
			() => true,
			() => "abc-id",
		);
		await flushAllMicrotasks();
		await runFactory();
		expect(spawnMock).toHaveBeenCalledTimes(1);
		const [cmd, args] = spawnMock.mock.calls[0] ?? [];
		expect(cmd).toBe("gaffer");
		expect(args).toEqual(["--invoker-id=abc-id", "--invoked-by=vscode", "lsp"]);
	});

	it("omits linkage flags when getInvokerId returns null (opt-out)", async () => {
		setTrusted(true);
		spawnMock.mockImplementation(() => fakeChild());
		startLanguageClient(
			makeContext(),
			() => true,
			() => null,
		);
		await flushAllMicrotasks();
		await runFactory();
		const [, args] = spawnMock.mock.calls[0] ?? [];
		expect(args).toEqual(["lsp"]);
	});

	it("re-evaluates getInvokerId when vscode-languageclient restarts the server", async () => {
		// Drives the same code path vscode-languageclient hits when the
		// error-handler returns CloseAction.Restart: it re-invokes
		// ServerOptions. A mid-session opt-out flips invokerId between
		// the two factory calls, and the second spawn must omit the
		// flag.
		setTrusted(true);
		spawnMock.mockImplementation(() => fakeChild());
		let current: string | null = "id-1";
		startLanguageClient(
			makeContext(),
			() => true,
			() => current,
		);
		await flushAllMicrotasks();
		const c = getLanguageClient() as unknown as LanguageClient;
		await (c.serverOptions as () => Promise<unknown>)(); // initial start
		current = null;
		await c.simulateRestart(); // simulates CloseAction.Restart re-entry

		const firstArgs = spawnMock.mock.calls[0]?.[1] as string[];
		const secondArgs = spawnMock.mock.calls[1]?.[1] as string[];
		expect(firstArgs).toContain("--invoker-id=id-1");
		expect(secondArgs).toEqual(["lsp"]);
	});
});

describe("startLanguageClient dispose-during-start race", () => {
	afterEach(async () => {
		await stopLanguageClient();
		resetVscode();
		resetLspMock();
		spawnMock.mockReset();
	});

	it("does not promote the client to module-level when disposed mid-start", async () => {
		// Holds `LanguageClient.start()` open so we can fire the
		// subscription's dispose between push-disposable and resume.
		// Production guard: spawnLanguageClient sets `disposed=true`
		// from the dispose callback and skips `client = c` on resume.
		setTrusted(true);
		spawnMock.mockImplementation(() => new EventEmitter());
		const release = holdLspStart();
		const ctx = makeContext();
		startLanguageClient(
			ctx,
			() => true,
			() => null,
		);
		await flushAllMicrotasks();
		// The dispose-handle is the bare `{ dispose }` object
		// spawnLanguageClient pushes; the other LSP-related subscriptions
		// quack like OutputChannel or vscode disposables. Find by shape
		// rather than position so a future push-order tweak doesn't
		// silently exercise the wrong subscription.
		const subs = ctx.subscriptions as unknown as Array<Record<string, unknown>>;
		const lspDispose = subs.find(
			(s) =>
				typeof s.dispose === "function" &&
				!("appendLine" in s) &&
				!("event" in s),
		);
		if (!lspDispose) throw new Error("expected an LSP dispose subscription");
		await (lspDispose.dispose as () => unknown)();
		release();
		await flushAllMicrotasks();
		expect(getLanguageClient()).toBeUndefined();
	});
});

describe("makeErrorHandler.closed restart budget", () => {
	it("returns Restart for the first 4 closes within the window", async () => {
		const h = makeErrorHandler(stubChannel);
		for (let i = 0; i < 4; i++) {
			const r = await h.closed();
			expect(r.action).toBe(CloseAction.Restart);
			expect(r.handled).toBe(true);
		}
	});

	it("returns DoNotRestart on the 5th close and surfaces a single toast", async () => {
		const h = makeErrorHandler(stubChannel);
		// Burn 4 attempts.
		for (let i = 0; i < 4; i++) {
			await h.closed();
		}
		// Toast hasn't fired yet (only logs).
		expect(
			getShownMessages().some((m) => /keeps crashing/.test(m.message)),
		).toBe(false);
		const r = await h.closed();
		expect(r.action).toBe(CloseAction.DoNotRestart);
		expect(r.handled).toBe(true);
		expect(
			getShownMessages().some((m) => /keeps crashing/.test(m.message)),
		).toBe(true);
	});

	it("only shows the give-up toast once across multiple closed() calls", async () => {
		// Once the budget is exhausted, additional closed() calls
		// (vscode-languageclient may queue more close events during
		// teardown) shouldn't re-fire the toast. The latch lives
		// inside the closure so it survives across calls.
		const h = makeErrorHandler(stubChannel);
		for (let i = 0; i < 6; i++) {
			await h.closed();
		}
		const matches = getShownMessages().filter((m) =>
			/keeps crashing/.test(m.message),
		);
		expect(matches).toHaveLength(1);
	});
});

describe("makeErrorHandler.error", () => {
	it("returns Continue with handled:true for low error counts", async () => {
		const h = makeErrorHandler(stubChannel);
		const r = await h.error(new Error("blip"), undefined, 1);
		expect(r.action).toBe(ErrorAction.Continue);
		expect(r.handled).toBe(true);
	});

	it("returns Continue at the count==3 boundary", async () => {
		// The boundary check is `count <= 3` -> Continue. Pin both
		// sides so an off-by-one regression is caught.
		const h = makeErrorHandler(stubChannel);
		const r3 = await h.error(new Error("blip"), undefined, 3);
		expect(r3.action).toBe(ErrorAction.Continue);
		const r4 = await h.error(new Error("blip"), undefined, 4);
		expect(r4.action).toBe(ErrorAction.Shutdown);
	});

	it("handles undefined count by returning Shutdown", async () => {
		// Defensive branch: vscode-languageclient might not always
		// supply a count. The current code's `count !== undefined &&
		// count <= 3` short-circuits to Shutdown when count is
		// undefined; pin so the next refactor doesn't accidentally
		// flip to Continue (which would mask transport errors).
		const h = makeErrorHandler(stubChannel);
		const r = await h.error(new Error("blip"), undefined, undefined);
		expect(r.action).toBe(ErrorAction.Shutdown);
		expect(r.handled).toBe(true);
	});
});
