import type * as vscode from "vscode";
import { describe, expect, it } from "vitest";
import { dispatchDapEvent } from "./dap-dispatch.js";
import type { StateProvider } from "../panels/state.js";
import type { StatusViewProvider } from "../panels/status.js";
import type { StepProvider } from "../panels/step.js";
import type { PhaseTracker } from "./phase-tracker.js";
import type { EmittedEvent, InputEvent, StepResult } from "../ipc/schemas.js";
import type { FinalStateBody, StateBody } from "./schemas.js";

interface RecordedStep {
	startStep: InputEvent[];
	addLog: string[];
	addEmit: EmittedEvent[];
	addWarning: Array<{ code: string; message: string }>;
	setResult: StepResult[];
	setError: Array<{ code: string; description: string }>;
}

interface RecordedState {
	setDebugSession: number;
	updateFromState: StateBody[];
	hydrateFinalState: FinalStateBody[];
}

function fakeStep(): { provider: StepProvider; calls: RecordedStep } {
	const calls: RecordedStep = {
		startStep: [],
		addLog: [],
		addEmit: [],
		addWarning: [],
		setResult: [],
		setError: [],
	};
	const provider = {
		startStep: (e: InputEvent) => calls.startStep.push(e),
		addLog: (m: string) => calls.addLog.push(m),
		addEmit: (e: EmittedEvent) => calls.addEmit.push(e),
		addWarning: (code: string, message: string) =>
			calls.addWarning.push({ code, message }),
		setResult: (r: StepResult) => calls.setResult.push(r),
		setError: (code: string, description: string) =>
			calls.setError.push({ code, description }),
	} as unknown as StepProvider;
	return { provider, calls };
}

function fakeState(): { provider: StateProvider; calls: RecordedState } {
	const calls: RecordedState = {
		setDebugSession: 0,
		updateFromState: [],
		hydrateFinalState: [],
	};
	const provider = {
		setDebugSession: () => {
			calls.setDebugSession++;
		},
		updateFromState: (b: StateBody) => calls.updateFromState.push(b),
		hydrateFinalState: (b: FinalStateBody) => calls.hydrateFinalState.push(b),
	} as unknown as StateProvider;
	return { provider, calls };
}

interface RecordedStatus {
	setStats: Array<{ processed: number; errors: number; quirks: number }>;
	setSkipped: Array<{ count: number; byReason: Record<string, number> }>;
}

function fakeStatus(): { provider: StatusViewProvider; calls: RecordedStatus } {
	const calls: RecordedStatus = { setStats: [], setSkipped: [] };
	const provider = {
		setStats: (processed: number, errors: number, quirks = 0) =>
			calls.setStats.push({ processed, errors, quirks }),
		setSkipped: (count: number, byReason: Record<string, number>) =>
			calls.setSkipped.push({ count, byReason }),
	} as unknown as StatusViewProvider;
	return { provider, calls };
}

interface RecordedTracker {
	noteSignal: number;
	setCaughtUp: boolean[];
}

function fakeTracker(): { tracker: PhaseTracker; calls: RecordedTracker } {
	const calls: RecordedTracker = { noteSignal: 0, setCaughtUp: [] };
	const tracker = {
		noteSignal: () => {
			calls.noteSignal++;
		},
		setCaughtUp: (b: boolean) => calls.setCaughtUp.push(b),
	} as unknown as PhaseTracker;
	return { tracker, calls };
}

const session = {
	id: "1",
	type: "gaffer",
	name: "checkout",
	configuration: {},
	customRequest: () => Promise.resolve(undefined),
} as unknown as vscode.DebugSession;

function event(name: string, body: unknown): vscode.DebugSessionCustomEvent {
	return { session, event: name, body };
}

const handlers = (overrides: {
	step?: StepProvider;
	state?: StateProvider;
	status?: StatusViewProvider;
	tracker?: PhaseTracker;
	setEngineMode?: (m: "running" | "inspecting") => Promise<void> | void;
}) =>
	({
		stepProvider: overrides.step ?? fakeStep().provider,
		stateProvider: overrides.state ?? fakeState().provider,
		statusProvider: overrides.status ?? fakeStatus().provider,
		phaseTracker: overrides.tracker ?? fakeTracker().tracker,
		setEngineMode: overrides.setEngineMode ?? (() => {}),
	}) as Parameters<typeof dispatchDapEvent>[1];

describe("dispatchDapEvent - non-gaffer sessions", () => {
	it("ignores events from non-gaffer sessions", async () => {
		// Locks in the early-return: another extension's debug session
		// firing custom events must not leak into our state, including
		// the phase tracker.
		const step = fakeStep();
		const state = fakeState();
		const tracker = fakeTracker();
		await dispatchDapEvent(
			{
				...event("gaffer/stepStart", {
					event: { sequenceNumber: 1, streamId: "s", eventType: "T" },
				}),
				session: { ...session, type: "node" },
			},
			handlers({
				step: step.provider,
				state: state.provider,
				tracker: tracker.tracker,
			}),
		);
		expect(step.calls.startStep).toEqual([]);
		expect(state.calls.setDebugSession).toBe(0);
		expect(tracker.calls.noteSignal).toBe(0);
	});
});

describe("dispatchDapEvent - happy paths", () => {
	it("captures the session reference on every event", async () => {
		const state = fakeState();
		await dispatchDapEvent(
			event("gaffer/stepLog", { message: "hi" }),
			handlers({ state: state.provider }),
		);
		expect(state.calls.setDebugSession).toBe(1);
	});

	it("routes gaffer/stepStart to startStep", async () => {
		const step = fakeStep();
		const e: InputEvent = {
			sequenceNumber: 1,
			streamId: "orders-1",
			eventType: "OrderPlaced",
		};
		await dispatchDapEvent(
			event("gaffer/stepStart", { event: e }),
			handlers({ step: step.provider }),
		);
		expect(step.calls.startStep).toEqual([e]);
	});

	it("routes gaffer/stepLog to addLog", async () => {
		const step = fakeStep();
		await dispatchDapEvent(
			event("gaffer/stepLog", { message: "hi" }),
			handlers({ step: step.provider }),
		);
		expect(step.calls.addLog).toEqual(["hi"]);
	});

	it("routes gaffer/stepEmit to addEmit", async () => {
		const step = fakeStep();
		const body: EmittedEvent = { streamId: "out-1", eventType: "Out" };
		await dispatchDapEvent(
			event("gaffer/stepEmit", body),
			handlers({ step: step.provider }),
		);
		expect(step.calls.addEmit).toEqual([body]);
	});

	it("routes gaffer/stepResult to setResult", async () => {
		const step = fakeStep();
		await dispatchDapEvent(
			event("gaffer/stepResult", {
				result: { status: "processed", state: { count: 1 } },
			}),
			handlers({ step: step.provider }),
		);
		expect(step.calls.setResult).toEqual([
			{ status: "processed", state: { count: 1 } },
		]);
	});

	it("routes gaffer/stepWarning to addWarning", async () => {
		const step = fakeStep();
		await dispatchDapEvent(
			event("gaffer/stepWarning", {
				step: 3,
				code: "quirk.serialize.rawString",
				message: "raw string JSON-quoted in slot 0",
				severity: 2,
			}),
			handlers({ step: step.provider }),
		);
		expect(step.calls.addWarning).toEqual([
			{
				code: "quirk.serialize.rawString",
				message: "raw string JSON-quoted in slot 0",
			},
		]);
	});

	it("routes gaffer/stepError to setError", async () => {
		const step = fakeStep();
		await dispatchDapEvent(
			event("gaffer/stepError", { code: "E_FOO", description: "boom" }),
			handlers({ step: step.provider }),
		);
		expect(step.calls.setError).toEqual([
			{ code: "E_FOO", description: "boom" },
		]);
	});

	it("routes gaffer/state to updateFromState", async () => {
		const state = fakeState();
		const body: StateBody = { state: { x: 1 }, partitions: ["p1"] };
		await dispatchDapEvent(
			event("gaffer/state", body),
			handlers({ state: state.provider }),
		);
		expect(state.calls.updateFromState).toEqual([body]);
	});

	it("routes gaffer/finalState to hydrateFinalState", async () => {
		const state = fakeState();
		const body = {
			partitions: { p1: { state: { count: 1 } } },
		};
		await dispatchDapEvent(
			event("gaffer/finalState", body),
			handlers({ state: state.provider }),
		);
		expect(state.calls.hydrateFinalState).toEqual([body]);
	});

	it("translates gaffer/mode 'inspect' to inspecting; everything else to running", async () => {
		// ModeBodySchema only validates that `mode` is a string, not its
		// value. Production maps `"inspect"` -> "inspecting" and ANY other
		// non-empty string falls through to "running". Test the contract
		// honestly so a future schema tightening (v.picklist) doesn't
		// silently break the assumption.
		const modes: Array<"running" | "inspecting"> = [];
		const push = (m: "running" | "inspecting"): void => void modes.push(m);
		await dispatchDapEvent(
			event("gaffer/mode", { mode: "inspect" }),
			handlers({ setEngineMode: push }),
		);
		await dispatchDapEvent(
			event("gaffer/mode", { mode: "run" }),
			handlers({ setEngineMode: push }),
		);
		await dispatchDapEvent(
			event("gaffer/mode", { mode: "wonky" }),
			handlers({ setEngineMode: push }),
		);
		expect(modes).toEqual(["inspecting", "running", "running"]);
	});

	it("routes gaffer/stats to setStats", async () => {
		const status = fakeStatus();
		await dispatchDapEvent(
			event("gaffer/stats", { handled: 12, errors: 1 }),
			handlers({ status: status.provider }),
		);
		expect(status.calls.setStats).toEqual([
			{ processed: 12, errors: 1, quirks: 0 },
		]);
	});

	it("routes gaffer/stats quirks count to setStats", async () => {
		const status = fakeStatus();
		await dispatchDapEvent(
			event("gaffer/stats", { handled: 5, errors: 0, quirks: 2 }),
			handlers({ status: status.provider }),
		);
		expect(status.calls.setStats).toEqual([
			{ processed: 5, errors: 0, quirks: 2 },
		]);
	});

	it("routes gaffer/stats with skipped fields to setSkipped", async () => {
		// Fixture mode case: CLI includes skipped + skippedByReason.
		const status = fakeStatus();
		await dispatchDapEvent(
			event("gaffer/stats", {
				handled: 3,
				errors: 0,
				skipped: 2,
				skippedByReason: { "wrong-stream": 2 },
			}),
			handlers({ status: status.provider }),
		);
		expect(status.calls.setSkipped).toEqual([
			{ count: 2, byReason: { "wrong-stream": 2 } },
		]);
	});

	it("does not call setSkipped when neither skipped field is present", async () => {
		// Live mode case: CLI omits the fields. The panel's skipped count
		// is reset() at session start, so we don't need a per-event clear.
		const status = fakeStatus();
		await dispatchDapEvent(
			event("gaffer/stats", { handled: 12, errors: 1 }),
			handlers({ status: status.provider }),
		);
		expect(status.calls.setSkipped).toEqual([]);
	});

	it("routes gaffer/stats with only skipped (no byReason) to setSkipped(n, {})", async () => {
		const status = fakeStatus();
		await dispatchDapEvent(
			event("gaffer/stats", { handled: 1, errors: 0, skipped: 4 }),
			handlers({ status: status.provider }),
		);
		expect(status.calls.setSkipped).toEqual([{ count: 4, byReason: {} }]);
	});

	it("routes gaffer/caughtUp to phaseTracker.setCaughtUp", async () => {
		const tracker = fakeTracker();
		await dispatchDapEvent(
			event("gaffer/caughtUp", { caughtUp: true }),
			handlers({ tracker: tracker.tracker }),
		);
		await dispatchDapEvent(
			event("gaffer/caughtUp", { caughtUp: false }),
			handlers({ tracker: tracker.tracker }),
		);
		expect(tracker.calls.setCaughtUp).toEqual([true, false]);
	});

	it("any gaffer/* event flips phaseTracker out of Connecting", async () => {
		// Specific signals (stats / caughtUp) are what move the phase
		// from Catching up <-> Caught up. But the user might be paused
		// at a breakpoint with no live stats / caughtUp - just stepStart
		// and stepResult firing. Any custom event still proves the CLI
		// is talking, so the dispatcher calls noteSignal unconditionally.
		const tracker = fakeTracker();
		await dispatchDapEvent(
			event("gaffer/stepStart", {
				event: { sequenceNumber: 1, streamId: "s", eventType: "T" },
			}),
			handlers({ tracker: tracker.tracker }),
		);
		expect(tracker.calls.noteSignal).toBeGreaterThan(0);
	});

	it("awaits setEngineMode before returning", async () => {
		let resolved = false;
		await dispatchDapEvent(
			event("gaffer/mode", { mode: "run" }),
			handlers({
				setEngineMode: async () => {
					await new Promise((r) => setTimeout(r, 1));
					resolved = true;
				},
			}),
		);
		expect(resolved).toBe(true);
	});
});

describe("dispatchDapEvent - malformed bodies", () => {
	const malformed: Array<[string, unknown]> = [
		["gaffer/stepStart", { event: { sequenceNumber: "x" } }],
		["gaffer/stepLog", { message: 1 }],
		[
			"gaffer/stepEmit",
			{
				/* missing streamId */
			},
		],
		["gaffer/stepResult", { result: { status: "weird" } }],
		["gaffer/stepError", { code: "x" /* missing description */ }],
		["gaffer/state", { partitions: "not-an-array" }],
		["gaffer/finalState", { partitions: "not-a-record" }],
		["gaffer/mode", { mode: 42 }],
		["gaffer/stats", { handled: "many", errors: 0 }],
		["gaffer/stats", { handled: 1, errors: 0, skipped: "two" }],
		["gaffer/stats", { handled: 1, errors: 0, skipped: -1 }],
		["gaffer/stats", { handled: 1, errors: 0, skipped: 1.5 }],
		[
			"gaffer/stats",
			{ handled: 1, errors: 0, skippedByReason: { "wrong-stream": "1" } },
		],
		[
			"gaffer/stats",
			{ handled: 1, errors: 0, skippedByReason: { "wrong-stream": -1 } },
		],
		["gaffer/caughtUp", { caughtUp: "yes" }],
	];

	for (const [name, body] of malformed) {
		it(`drops the dispatch for malformed ${name}`, async () => {
			const step = fakeStep();
			const state = fakeState();
			const status = fakeStatus();
			let modeCalled = false;
			await dispatchDapEvent(
				event(name, body),
				handlers({
					step: step.provider,
					state: state.provider,
					status: status.provider,
					setEngineMode: () => {
						modeCalled = true;
					},
				}),
			);
			expect(step.calls.startStep).toEqual([]);
			expect(step.calls.addLog).toEqual([]);
			expect(step.calls.addEmit).toEqual([]);
			expect(step.calls.setResult).toEqual([]);
			expect(step.calls.setError).toEqual([]);
			expect(state.calls.updateFromState).toEqual([]);
			expect(status.calls.setStats).toEqual([]);
			expect(status.calls.setSkipped).toEqual([]);
			expect(modeCalled).toBe(false);
			// setDebugSession fires before the per-event parse since
			// every gaffer event needs the session reference for
			// downstream customRequest calls.
			expect(state.calls.setDebugSession).toBe(1);
		});
	}
});

describe("dispatchDapEvent - unknown event names", () => {
	it("does nothing for an event not in the switch", async () => {
		const step = fakeStep();
		const state = fakeState();
		await dispatchDapEvent(
			event("gaffer/something-new", { x: 1 }),
			handlers({ step: step.provider, state: state.provider }),
		);
		// setDebugSession still fires (it's outside the switch) but
		// no router branch gets invoked.
		expect(state.calls.setDebugSession).toBe(1);
		expect(step.calls.startStep).toEqual([]);
	});
});
