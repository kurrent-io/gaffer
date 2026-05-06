import { afterEach, describe, expect, it } from "vitest";
import {
	CloseAction,
	ErrorAction,
} from "../../test/__mocks__/vscode-languageclient-node.js";
import { makeErrorHandler } from "./client.js";
import {
	getShownMessages,
	resetVscode,
} from "../../test/testutil/vscode-state.js";

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
