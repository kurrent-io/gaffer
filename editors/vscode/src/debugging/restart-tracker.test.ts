import * as vscode from "vscode";
import { describe, expect, it } from "vitest";
import { RestartTrackerFactory } from "./restart-tracker.js";
import type { StepProvider } from "../panels/step.js";
import type { StateProvider } from "../panels/state.js";
import type { StatusViewProvider } from "../panels/status.js";
import type { PhaseTracker } from "./phase-tracker.js";

interface Calls {
	stepClear: number;
	stateClear: number;
	statusReset: string[];
	phaseReset: number;
}

function fakes(): {
	deps: ConstructorParameters<typeof RestartTrackerFactory>[0];
	calls: Calls;
} {
	const calls: Calls = {
		stepClear: 0,
		stateClear: 0,
		statusReset: [],
		phaseReset: 0,
	};
	const deps = {
		stepProvider: {
			clear: () => {
				calls.stepClear++;
			},
		} as unknown as StepProvider,
		stateProvider: {
			clear: () => {
				calls.stateClear++;
			},
		} as unknown as StateProvider,
		statusProvider: {
			reset: (name: string) => calls.statusReset.push(name),
		} as unknown as StatusViewProvider,
		phaseTracker: {
			reset: () => {
				calls.phaseReset++;
			},
		} as unknown as PhaseTracker,
		sessionName: () => "checkout",
	};
	return { deps, calls };
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

function initEvent(): unknown {
	return { type: "event", event: "initialized" };
}

describe("RestartTrackerFactory", () => {
	it("returns undefined for non-gaffer sessions", () => {
		const { deps } = fakes();
		const tracker = new RestartTrackerFactory(deps).createDebugAdapterTracker({
			...gafferSession(),
			type: "node",
		} as unknown as vscode.DebugSession);
		expect(tracker).toBeUndefined();
	});

	it("ignores the first 'initialized' event (normal startup)", () => {
		const { deps, calls } = fakes();
		const tracker = new RestartTrackerFactory(deps).createDebugAdapterTracker(
			gafferSession(),
		);
		tracker?.onDidSendMessage?.(initEvent());
		expect(calls).toEqual({
			stepClear: 0,
			stateClear: 0,
			statusReset: [],
			phaseReset: 0,
		});
	});

	it("treats the second 'initialized' as a restart and resets all panels", () => {
		const { deps, calls } = fakes();
		const tracker = new RestartTrackerFactory(deps).createDebugAdapterTracker(
			gafferSession(),
		);
		tracker?.onDidSendMessage?.(initEvent()); // startup
		tracker?.onDidSendMessage?.(initEvent()); // restart
		expect(calls.stepClear).toBe(1);
		expect(calls.stateClear).toBe(1);
		expect(calls.statusReset).toEqual(["checkout"]);
		expect(calls.phaseReset).toBe(1);
	});

	it("resets again on each subsequent 'initialized' (multiple restarts)", () => {
		const { deps, calls } = fakes();
		const tracker = new RestartTrackerFactory(deps).createDebugAdapterTracker(
			gafferSession(),
		);
		tracker?.onDidSendMessage?.(initEvent()); // startup
		tracker?.onDidSendMessage?.(initEvent()); // restart 1
		tracker?.onDidSendMessage?.(initEvent()); // restart 2
		expect(calls.phaseReset).toBe(2);
		expect(calls.statusReset).toEqual(["checkout", "checkout"]);
	});

	it("ignores non-initialized events", () => {
		const { deps, calls } = fakes();
		const tracker = new RestartTrackerFactory(deps).createDebugAdapterTracker(
			gafferSession(),
		);
		tracker?.onDidSendMessage?.({ type: "event", event: "stopped" });
		tracker?.onDidSendMessage?.({ type: "response", command: "pause" });
		tracker?.onDidSendMessage?.({ type: "event", event: "gaffer/stats" });
		expect(calls).toEqual({
			stepClear: 0,
			stateClear: 0,
			statusReset: [],
			phaseReset: 0,
		});
	});

	it("ignores malformed messages", () => {
		const { deps, calls } = fakes();
		const tracker = new RestartTrackerFactory(deps).createDebugAdapterTracker(
			gafferSession(),
		);
		tracker?.onDidSendMessage?.(null);
		tracker?.onDidSendMessage?.("initialized");
		tracker?.onDidSendMessage?.({ type: "event" });
		expect(calls.phaseReset).toBe(0);
	});

	it("each session gets its own tracker (counter doesn't leak across sessions)", () => {
		// VS Code creates one tracker per session via the factory. Two
		// sessions in sequence should each see their first initialized
		// as startup, not as restart.
		const { deps, calls } = fakes();
		const factory = new RestartTrackerFactory(deps);
		const t1 = factory.createDebugAdapterTracker(gafferSession());
		t1?.onDidSendMessage?.(initEvent()); // session 1 startup
		t1?.onDidSendMessage?.(initEvent()); // session 1 restart
		const t2 = factory.createDebugAdapterTracker(gafferSession());
		t2?.onDidSendMessage?.(initEvent()); // session 2 startup - should NOT reset
		expect(calls.phaseReset).toBe(1);
	});
});
