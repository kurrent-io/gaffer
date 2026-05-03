import * as vscode from "vscode";
import { describe, expect, it } from "vitest";
import { PausePendingTrackerFactory } from "./pause-pending-tracker.js";
import type { StatusViewProvider } from "../panels/status.js";

function fakeStatus(): {
	provider: StatusViewProvider;
	calls: boolean[];
} {
	const calls: boolean[] = [];
	const provider = {
		setPausePending: (pending: boolean) => calls.push(pending),
	} as unknown as StatusViewProvider;
	return { provider, calls };
}

function gafferSession(): vscode.DebugSession {
	return {
		id: "1",
		type: "gaffer",
		name: "checkout",
		configuration: {},
		customRequest: () => Promise.resolve(undefined),
	} as unknown as vscode.DebugSession;
}

describe("PausePendingTrackerFactory", () => {
	it("returns undefined for non-gaffer sessions so other adapters aren't tracked", () => {
		const { provider } = fakeStatus();
		const factory = new PausePendingTrackerFactory(provider);
		const tracker = factory.createDebugAdapterTracker({
			...gafferSession(),
			type: "node",
		} as unknown as vscode.DebugSession);
		expect(tracker).toBeUndefined();
	});

	it("flips pending=true on outgoing pause requests", () => {
		const { provider, calls } = fakeStatus();
		const tracker = new PausePendingTrackerFactory(
			provider,
		).createDebugAdapterTracker(gafferSession());
		tracker?.onWillReceiveMessage?.({
			type: "request",
			command: "pause",
			seq: 1,
		});
		expect(calls).toEqual([true]);
	});

	it("flips pending=false on incoming stopped events", () => {
		const { provider, calls } = fakeStatus();
		const tracker = new PausePendingTrackerFactory(
			provider,
		).createDebugAdapterTracker(gafferSession());
		tracker?.onDidSendMessage?.({
			type: "event",
			event: "stopped",
			body: { reason: "pause" },
		});
		expect(calls).toEqual([false]);
	});

	it("ignores unrelated requests and events", () => {
		// Other DAP traffic (continue, threads, custom events) shouldn't
		// touch pending state - only pause requests and stopped events
		// drive the indicator.
		const { provider, calls } = fakeStatus();
		const tracker = new PausePendingTrackerFactory(
			provider,
		).createDebugAdapterTracker(gafferSession());
		tracker?.onWillReceiveMessage?.({
			type: "request",
			command: "continue",
		});
		tracker?.onWillReceiveMessage?.({
			type: "request",
			command: "threads",
		});
		tracker?.onDidSendMessage?.({ type: "event", event: "output" });
		tracker?.onDidSendMessage?.({ type: "event", event: "gaffer/stats" });
		expect(calls).toEqual([]);
	});

	it("ignores malformed messages", () => {
		const { provider, calls } = fakeStatus();
		const tracker = new PausePendingTrackerFactory(
			provider,
		).createDebugAdapterTracker(gafferSession());
		tracker?.onWillReceiveMessage?.(null);
		tracker?.onWillReceiveMessage?.("pause");
		tracker?.onDidSendMessage?.({ type: "event" /* missing event */ });
		expect(calls).toEqual([]);
	});

	it("the same flow drives all three pause entry points (panel, toolbar, F6)", () => {
		// All three funnel through the same outgoing DAP `pause`
		// request, which is exactly what the tracker hooks on. So a
		// single onWillReceiveMessage with command=pause covers
		// everything; this test just locks that contract in.
		const { provider, calls } = fakeStatus();
		const tracker = new PausePendingTrackerFactory(
			provider,
		).createDebugAdapterTracker(gafferSession());
		for (let i = 0; i < 3; i++) {
			tracker?.onWillReceiveMessage?.({
				type: "request",
				command: "pause",
				seq: i,
			});
		}
		expect(calls).toEqual([true, true, true]);
	});
});
