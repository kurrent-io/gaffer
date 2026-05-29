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
	pauseButtonLabel: string;
	pauseButtonDisabled: boolean;
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
	it("posts an initial update with the running title, a Connecting placeholder, and the pause button hidden", () => {
		// Initial phase is connecting; no events possible yet so the
		// pause button would be a black hole - hide it until the first
		// signal arrives.
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		const update = lastUpdate(view);
		expect(update.mode).toBe("running");
		expect(update.title).toBe("Running projection...");
		expect(update.stats).toEqual(["Connecting..."]);
		expect(update.showPauseButton).toBe(false);
	});

	it("setPhase('catching-up') replaces the Connecting placeholder with Waiting for events and shows the pause button", () => {
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		provider.setPhase("catching-up");
		const update = lastUpdate(view);
		expect(update.stats).toEqual(["Waiting for events..."]);
		expect(update.showPauseButton).toBe(true);
	});

	it("setPhase('disconnected') hides the pause button even with mode=running", () => {
		// Defensive: idle cleanup ends the phase tracker without
		// flipping mode to ended. The panel is hidden in that path
		// via gaffer.mode, but if it's ever shown we don't want a
		// clickable pause button on a dead DAP socket.
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		provider.setPhase("disconnected");
		const update = lastUpdate(view);
		expect(update.mode).toBe("running");
		expect(update.showPauseButton).toBe(false);
	});

	it("setPhase is a no-op when phase is unchanged", () => {
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		const before = view.webview.postedMessages.length;
		provider.setPhase("connecting");
		expect(view.webview.postedMessages.length).toBe(before);
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

	it("setPhase writes the label through to the webviewView; survives view re-resolve", () => {
		const provider = new StatusViewProvider();
		const view1 = makeFakeWebviewView();
		provider.resolveWebviewView(view1 as unknown as vscode.WebviewView);
		provider.setPhase("catching-up");
		expect(view1.description).toBe("Catching up");

		// View flips out (e.g. when-clause toggles in inspecting mode)
		// and a new one resolves. The cached phase is re-applied.
		const view2 = makeFakeWebviewView();
		provider.resolveWebviewView(view2 as unknown as vscode.WebviewView);
		expect(view2.description).toBe("Catching up");
	});

	it("includes processed/errors in stats when non-zero", () => {
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		provider.reset("checkout");
		provider.setStats(1, 1);
		expect(lastUpdate(view).stats).toEqual(["1 events processed", "1 errors"]);
	});

	it("includes a quirks line when quirks fired, singular at 1", () => {
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		provider.reset("checkout");
		provider.setStats(3, 0, 1);
		expect(lastUpdate(view).stats).toEqual(["3 events processed", "1 quirk"]);
		provider.setStats(3, 0, 2);
		expect(lastUpdate(view).stats).toEqual(["3 events processed", "2 quirks"]);
	});

	it("setSkipped surfaces a skipped line with the by-reason breakdown", () => {
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		provider.reset("checkout");
		provider.setStats(1, 0);
		provider.setSkipped(5, { "wrong-stream": 3, "no-handler": 2 });
		expect(lastUpdate(view).stats).toEqual([
			"1 events processed",
			"5 skipped (3 wrong-stream, 2 no-handler)",
		]);
	});

	it("renders skipped without breakdown when byReason is empty", () => {
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		provider.reset("checkout");
		provider.setSkipped(2, {});
		expect(lastUpdate(view).stats).toEqual(["2 skipped"]);
	});

	it("orders the breakdown by descending count, not alphabetical", () => {
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		provider.setSkipped(7, { deleted: 1, "wrong-stream": 4, "no-handler": 2 });
		expect(lastUpdate(view).stats).toEqual([
			"7 skipped (4 wrong-stream, 2 no-handler, 1 deleted)",
		]);
	});

	it("caps the breakdown at three entries with a +N more suffix", () => {
		// Five different reasons compresses to top-3 + "+2 more". Keeps
		// the line scannable on long fixture runs without losing the
		// signal that the breakdown is richer.
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		provider.setSkipped(15, {
			"wrong-stream": 5,
			"no-handler": 4,
			deleted: 3,
			"system-event": 2,
			other: 1,
		});
		expect(lastUpdate(view).stats).toEqual([
			"15 skipped (5 wrong-stream, 4 no-handler, 3 deleted, +2 more)",
		]);
	});

	it("setSkipped(0, {}) does not surface a skipped line", () => {
		// Live mode: CLI omits the fields, dispatcher defaults to 0 / {}.
		// Don't render a "0 skipped" line in that case.
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		provider.reset("checkout");
		provider.setStats(3, 0);
		provider.setSkipped(0, {});
		expect(lastUpdate(view).stats).toEqual(["3 events processed"]);
	});

	it("setSkipped is a no-op when neither count nor byReason changes", () => {
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		provider.setSkipped(2, { "wrong-stream": 2 });
		const beforeCount = view.webview.postedMessages.length;
		provider.setSkipped(2, { "wrong-stream": 2 });
		expect(view.webview.postedMessages.length).toBe(beforeCount);
	});

	it("setSkipped re-renders when byReason changes even if count is unchanged", () => {
		// A reason transition (e.g. only "wrong-stream" -> only "deleted")
		// at the same count is still meaningful - the panel may grow a
		// breakdown over time.
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		provider.setSkipped(2, { "wrong-stream": 2 });
		const beforeCount = view.webview.postedMessages.length;
		provider.setSkipped(2, { deleted: 2 });
		expect(view.webview.postedMessages.length).toBe(beforeCount + 1);
	});

	it("reset() clears skipped count", () => {
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		provider.setSkipped(2, { "wrong-stream": 2 });
		provider.reset("checkout");
		const update = lastUpdate(view);
		expect(update.stats).not.toContain("2 skipped");
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

	it("setPausePending(true) disables the button and changes the label to 'Waiting for event to pause'", () => {
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		provider.setPhase("catching-up");
		provider.setPausePending(true);
		const update = lastUpdate(view);
		expect(update.pauseButtonDisabled).toBe(true);
		expect(update.pauseButtonLabel).toBe("Waiting for event to pause");
		expect(update.showPauseButton).toBe(true);
	});

	it("setPausePending(false) restores the default label and re-enables the button", () => {
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		provider.setPhase("catching-up");
		provider.setPausePending(true);
		provider.setPausePending(false);
		const update = lastUpdate(view);
		expect(update.pauseButtonDisabled).toBe(false);
		expect(update.pauseButtonLabel).toBe("Pause at next event");
	});

	it("setPausePending is a no-op when state is unchanged", () => {
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		const beforeCount = view.webview.postedMessages.length;
		provider.setPausePending(false);
		expect(view.webview.postedMessages.length).toBe(beforeCount);
	});

	it("reset() clears a stuck pause-pending state", () => {
		// Disconnects mid-pending shouldn't carry the state into the
		// next session - the button needs to start clickable.
		const provider = new StatusViewProvider();
		const view = makeFakeWebviewView();
		provider.resolveWebviewView(view as unknown as vscode.WebviewView);
		provider.setPausePending(true);
		provider.reset("checkout");
		const update = lastUpdate(view);
		expect(update.pauseButtonDisabled).toBe(false);
		expect(update.pauseButtonLabel).toBe("Pause at next event");
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
