import * as vscode from "vscode";
import { describe, expect, it } from "vitest";
import { StatusViewProvider } from "./status.js";
import { makeFakeWebviewView } from "../../test/__mocks__/vscode.js";

interface UpdateMessage {
	type: "update";
	mode: "running" | "ended";
	title: string;
	stats: string[];
	showPauseButton: boolean;
}

function lastUpdate(
	view: ReturnType<typeof makeFakeWebviewView>,
): UpdateMessage {
	const messages = view.webview.postedMessages;
	const last = messages.at(-1);
	if (!last) throw new Error("no messages posted");
	return last as UpdateMessage;
}

describe("StatusViewProvider", () => {
	it("posts an initial update with the running title and a Waiting placeholder when resolved", () => {
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		const update = lastUpdate(view);
		expect(update.mode).toBe("running");
		expect(update.title).toBe("Running projection...");
		expect(update.stats).toEqual(["Waiting for events..."]);
		expect(update.showPauseButton).toBe(true);
	});

	it("populates the webview html with the CSP nonce and template substitution", () => {
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		expect(view.webview.html).toContain("default-src");
		expect(view.webview.html).not.toContain("{{NONCE}}");
		expect(view.webview.html).not.toContain("{{CSP_SOURCE}}");
	});

	it("reset(name) puts the projection name into the title and resets counters", () => {
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		provider.reset("checkout");
		const update = lastUpdate(view);
		expect(update.title).toBe("Running checkout...");
	});

	it("setStats posts a single update reflecting the cumulative totals", () => {
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		provider.reset("checkout");
		const beforeCount = view.webview.postedMessages.length;
		provider.setStats(3, 0);
		expect(view.webview.postedMessages.length).toBe(beforeCount + 1);
		const update = lastUpdate(view);
		expect(update.stats).toContain("3 events processed");
	});

	it("setStats is a no-op when nothing has changed", () => {
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		provider.setStats(5, 0);
		const beforeCount = view.webview.postedMessages.length;
		provider.setStats(5, 0);
		expect(view.webview.postedMessages.length).toBe(beforeCount);
	});

	it("includes processed/errors in stats when non-zero (skipped intentionally hidden)", () => {
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		provider.reset("checkout");
		provider.setStats(1, 1);
		expect(lastUpdate(view).stats).toEqual(["1 events processed", "1 errors"]);
	});

	it("markEnded flips mode to 'ended', updates title, and hides the pause button", () => {
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		provider.reset("checkout");
		provider.setStats(1, 0);
		provider.markEnded();
		const update = lastUpdate(view);
		expect(update.mode).toBe("ended");
		expect(update.title).toBe("Finished checkout");
		expect(update.showPauseButton).toBe(false);
	});

	it("re-renders with the right mode when the view is reconstructed", () => {
		const provider = new StatusViewProvider();
		const view1 = makeFakeWebviewView();
		provider.resolveWebviewView(view1 as unknown as vscode.WebviewView);
		provider.reset("checkout");
		provider.setStats(1, 0);
		provider.markEnded();
		// View flips out (e.g. when-clause toggles) and a new one resolves.
		const view2 = makeFakeWebviewView();
		provider.resolveWebviewView(view2 as unknown as vscode.WebviewView);
		const update = lastUpdate(view2);
		// Provider remembers ended mode + counters across view reconstruction.
		expect(update.mode).toBe("ended");
		expect(update.title).toBe("Finished checkout");
		expect(update.stats).toContain("1 events processed");
	});

	it("forwards the 'pause' webview message to workbench.action.debug.pause", async () => {
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		view.webview.emitMessage({ command: "pause" });
		// executeCommand is fire-and-forget via void; await microtasks.
		await Promise.resolve();
		const { getState } = await import("../../test/testutil/vscode-state.js");
		const calls = getState().executeCommandCalls;
		expect(calls.some((c) => c.name === "workbench.action.debug.pause")).toBe(
			true,
		);
	});

	it("ignores webview messages other than pause", async () => {
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		view.webview.emitMessage({ command: "garbage" });
		await Promise.resolve();
		const { getState } = await import("../../test/testutil/vscode-state.js");
		const calls = getState().executeCommandCalls;
		expect(calls.some((c) => c.name === "workbench.action.debug.pause")).toBe(
			false,
		);
	});
});
