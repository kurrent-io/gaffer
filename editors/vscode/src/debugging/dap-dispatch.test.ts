import type * as vscode from "vscode";
import { describe, expect, it } from "vitest";
import { dispatchDapEvent } from "./dap-dispatch.js";
import type { StateProvider } from "../panels/state.js";
import type { StatusViewProvider } from "../panels/status.js";
import type { StepProvider } from "../panels/step.js";
import type { EmittedEvent, InputEvent, StepResult } from "../ipc/schemas.js";
import type { StateBody } from "./schemas.js";

interface RecordedStep {
	startStep: InputEvent[];
	addLog: string[];
	addEmit: EmittedEvent[];
	setResult: StepResult[];
	setError: Array<{ code: string; description: string }>;
}

interface RecordedState {
	setDebugSession: number;
	updateFromState: StateBody[];
}

function fakeStep(): { provider: StepProvider; calls: RecordedStep } {
	const calls: RecordedStep = {
		startStep: [],
		addLog: [],
		addEmit: [],
		setResult: [],
		setError: [],
	};
	const provider = {
		startStep: (e: InputEvent) => calls.startStep.push(e),
		addLog: (m: string) => calls.addLog.push(m),
		addEmit: (e: EmittedEvent) => calls.addEmit.push(e),
		setResult: (r: StepResult) => calls.setResult.push(r),
		setError: (code: string, description: string) =>
			calls.setError.push({ code, description }),
	} as unknown as StepProvider;
	return { provider, calls };
}

function fakeState(): { provider: StateProvider; calls: RecordedState } {
	const calls: RecordedState = { setDebugSession: 0, updateFromState: [] };
	const provider = {
		setDebugSession: () => {
			calls.setDebugSession++;
		},
		updateFromState: (b: StateBody) => calls.updateFromState.push(b),
	} as unknown as StateProvider;
	return { provider, calls };
}

interface RecordedStatus {
	setStats: Array<{ processed: number; skipped: number; errors: number }>;
}

function fakeStatus(): { provider: StatusViewProvider; calls: RecordedStatus } {
	const calls: RecordedStatus = { setStats: [] };
	const provider = {
		setStats: (processed: number, skipped: number, errors: number) =>
			calls.setStats.push({ processed, skipped, errors }),
	} as unknown as StatusViewProvider;
	return { provider, calls };
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
	setEngineMode?: (m: "running" | "inspecting") => Promise<void> | void;
}) =>
	({
		stepProvider: overrides.step ?? fakeStep().provider,
		stateProvider: overrides.state ?? fakeState().provider,
		statusProvider: overrides.status ?? fakeStatus().provider,
		setEngineMode: overrides.setEngineMode ?? (() => {}),
	}) as Parameters<typeof dispatchDapEvent>[1];

describe("dispatchDapEvent - non-gaffer sessions", () => {
	it("ignores events from non-gaffer sessions", async () => {
		const step = fakeStep();
		const state = fakeState();
		await dispatchDapEvent(
			{
				...event("gaffer/stepStart", {
					event: { sequenceNumber: 1, streamId: "s", eventType: "T" },
				}),
				session: { ...session, type: "node" },
			},
			handlers({ step: step.provider, state: state.provider }),
		);
		expect(step.calls.startStep).toEqual([]);
		expect(state.calls.setDebugSession).toBe(0);
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
			event("gaffer/stats", { handled: 12, skipped: 3, errors: 1 }),
			handlers({ status: status.provider }),
		);
		expect(status.calls.setStats).toEqual([
			{ processed: 12, skipped: 3, errors: 1 },
		]);
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
		["gaffer/mode", { mode: 42 }],
		["gaffer/stats", { handled: "many", skipped: 0, errors: 0 }],
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
