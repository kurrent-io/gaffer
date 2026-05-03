import * as vscode from "vscode";
import { beforeEach, describe, expect, it } from "vitest";
import { SessionController } from "./session-controller.js";
import { FakeSession } from "../../test/testutil/fake-session.js";
import { makeContext } from "../../test/testutil/fake-context.js";
import {
	fireDebugStarted,
	fireDebugTerminated,
	getLastStartedDebugSession,
	getShownMessages,
	getState,
	setCommandHandler,
	setConfiguration,
	setStartDebuggingResult,
	setTrusted,
} from "../../test/testutil/vscode-state.js";
import { flushAllMicrotasks } from "../../test/testutil/promise.js";
import type { CreateSession, SessionLike } from "../ipc/session.js";
import type { StateProvider } from "../panels/state.js";
import type { StepProvider } from "../panels/step.js";
import type { StatusViewProvider } from "../panels/status.js";
import type { DebugState } from "../types.js";

interface ProviderCalls {
	step: { clear: number };
	state: { clear: number; markEnded: number; setDebugSessionCount: number };
	status: { reset: string[]; markEnded: number };
}

function fakeProviders(): {
	step: StepProvider;
	state: StateProvider;
	status: StatusViewProvider;
	calls: ProviderCalls;
} {
	const calls: ProviderCalls = {
		step: { clear: 0 },
		state: { clear: 0, markEnded: 0, setDebugSessionCount: 0 },
		status: { reset: [], markEnded: 0 },
	};
	const step = {
		clear: () => {
			calls.step.clear++;
		},
	} as unknown as StepProvider;
	const state = {
		clear: () => {
			calls.state.clear++;
		},
		markEnded: () => {
			calls.state.markEnded++;
		},
		setDebugSession: () => {
			calls.state.setDebugSessionCount++;
		},
	} as unknown as StateProvider;
	const status = {
		reset: (n: string) => {
			calls.status.reset.push(n);
		},
		markEnded: () => {
			calls.status.markEnded++;
		},
	} as unknown as StatusViewProvider;
	return { step, state, status, calls };
}

interface Harness {
	controller: SessionController;
	pushed: DebugState[];
	contextCalls: Array<unknown>;
	getActiveSession: () => FakeSession | null;
	providerCalls: ProviderCalls;
}

function makeHarness(): Harness {
	const providers = fakeProviders();
	const pushed: DebugState[] = [];
	let active: FakeSession | null = null;
	const factory: CreateSession = (name, argv, options) => {
		active = new FakeSession(name, argv, options);
		return active as SessionLike;
	};
	// Capture setContext calls for the gaffer.mode key.
	const contextCalls: unknown[] = [];
	setCommandHandler("setContext", (key, value) => {
		if (key === "gaffer.mode") contextCalls.push(value);
		return undefined;
	});
	const controller = new SessionController({
		buildArgv: (args) => ["gaffer", ...args],
		stepProvider: providers.step,
		stateProvider: providers.state,
		statusProvider: providers.status,
		// Defensive copy so the controller's internal state mutations
		// can't retroactively change the recorded value.
		pushDebugState: (s) => {
			pushed.push({ ...s });
		},
		createSession: factory,
	});
	const ctx = makeContext();
	controller.register(ctx);
	return {
		controller,
		pushed,
		contextCalls,
		getActiveSession: () => active,
		providerCalls: providers.calls,
	};
}

const projectionArgs = {
	name: "checkout",
	tomlUri: vscode.Uri.file("/p/checkout/gaffer.toml"),
};

// Drive controller.start() up to the point where it's awaiting
// session.waitForDebug(). Caller resumes by calling resolveDebug or
// rejectDebug on the returned FakeSession.
async function startUntilWaitForDebug(
	h: Harness,
): Promise<{ startPromise: Promise<void>; session: FakeSession }> {
	const startPromise = h.controller.start(projectionArgs);
	await flushAllMicrotasks();
	const session = h.getActiveSession();
	if (!session) throw new Error("expected createSession to have been called");
	return { startPromise, session };
}

// Drive a clean start through to running. The mock's startDebugging
// fires onDidStartDebugSession internally (matching production order),
// so the controller's identity-capture listener picks up the auto-
// constructed DebugSession. Returns it so terminate-identity tests can
// reference the same instance.
async function startToRunning(h: Harness): Promise<{
	session: FakeSession;
	debugSession: vscode.DebugSession;
}> {
	const { startPromise, session } = await startUntilWaitForDebug(h);
	session.resolveDebug(4711);
	await startPromise;
	const debugSession = getLastStartedDebugSession();
	if (!debugSession) throw new Error("expected startDebugging to have fired");
	return {
		session,
		debugSession: debugSession as unknown as vscode.DebugSession,
	};
}

beforeEach(() => {
	setTrusted(true);
});

describe("SessionController.start - happy path", () => {
	it("transitions idle -> starting -> running and pushes debug state at each step", async () => {
		const h = makeHarness();
		await startToRunning(h);
		expect(h.pushed.map((s) => s.status)).toEqual(["starting", "running"]);
		expect(h.pushed.map((s) => s.name)).toEqual(["checkout", "checkout"]);
	});

	it("sets gaffer.mode context to undefined for starting and 'running' for running", async () => {
		const h = makeHarness();
		await startToRunning(h);
		expect(h.contextCalls).toEqual([undefined, "running"]);
	});

	it("resets the status provider with the projection name", async () => {
		const h = makeHarness();
		await startToRunning(h);
		expect(h.providerCalls.status.reset).toEqual(["checkout"]);
	});

	it("uses the configured debug port via gaffer.debugPort", async () => {
		setConfiguration("gaffer", "debugPort", { value: 5555 });
		const h = makeHarness();
		const { session } = await startToRunning(h);
		expect(session.argv).toContain("--debug-port");
		const idx = session.argv.indexOf("--debug-port");
		expect(session.argv[idx + 1]).toBe("5555");
	});

	it("falls back to 4711 when gaffer.debugPort is unset", async () => {
		const h = makeHarness();
		const { session } = await startToRunning(h);
		const idx = session.argv.indexOf("--debug-port");
		expect(session.argv[idx + 1]).toBe("4711");
	});

	it("passes --start-paused-if-no-breakpoints by default", async () => {
		// Extension's UX default: lands the user in `inspecting` mode
		// immediately so the State view is populated before any events
		// are processed. With breakpoints set, the CLI runs to first hit.
		const h = makeHarness();
		const { session } = await startToRunning(h);
		expect(session.argv).toContain("--start-paused-if-no-breakpoints");
	});

	it("clears diagnostics at start() (not in cleanup)", async () => {
		// Pre-populate a diagnostic so we can observe whether start clears it.
		const { reportFatalError, initDiagnostics } =
			await import("../diagnostics.js");
		initDiagnostics(makeContext());
		reportFatalError({
			file: "/p/checkout/projection.js",
			line: 1,
			column: 1,
			code: "JS_ERROR",
			description: "compile failed",
			jsStack: undefined,
			eventId: undefined,
		});
		expect(getState().diagnosticCollections[0]?.entries.size).toBe(1);

		const h = makeHarness();
		await startToRunning(h);
		expect(getState().diagnosticCollections[0]?.entries.size).toBe(0);
	});
});

describe("SessionController.start - guards", () => {
	it("does not create a session when workspace is untrusted; shows trust warning", async () => {
		setTrusted(false);
		const h = makeHarness();
		await h.controller.start(projectionArgs);
		expect(h.getActiveSession()).toBeNull();
		expect(h.pushed).toEqual([]);
	});

	it("ignores start() when already starting", async () => {
		const h = makeHarness();
		const { startPromise, session } = await startUntilWaitForDebug(h);
		// Second start while in 'starting' should resolve quickly without
		// creating a second session.
		const second = h.controller.start(projectionArgs);
		let secondResolved = false;
		void second.then(() => {
			secondResolved = true;
		});
		await flushAllMicrotasks();
		expect(secondResolved).toBe(true);
		expect(h.getActiveSession()).toBe(session);
		// Drain the first start so afterEach is clean.
		session.resolveDebug(4711);
		await startPromise;
	});

	it("ignores start() when running", async () => {
		const h = makeHarness();
		const { session } = await startToRunning(h);
		const second = h.controller.start(projectionArgs);
		await flushAllMicrotasks();
		await second;
		// Same session - no second factory call.
		expect(h.getActiveSession()).toBe(session);
	});

	it("treats start() from ended as a fresh idle->starting transition", async () => {
		const h = makeHarness();
		const { session } = await startToRunning(h);
		// Force ended via exit.
		session.fire({ type: "exit", code: 0 });
		await flushAllMicrotasks();
		expect(h.pushed.at(-1)?.status).toBe("ended");

		// Restart - new session expected.
		const { session: second } = await startToRunning(h);
		expect(second).not.toBe(session);
		expect(h.pushed.map((s) => s.status)).toEqual([
			"starting",
			"running",
			"ended",
			"idle",
			"starting",
			"running",
		]);
	});
});

describe("SessionController.start - failures", () => {
	it("routes a waitForDebug rejection through cleanup('idle')", async () => {
		const h = makeHarness();
		const { startPromise, session } = await startUntilWaitForDebug(h);
		session.rejectDebug(new Error("CLI failed to start"));
		await startPromise;
		expect(h.pushed.at(-1)?.status).toBe("idle");
		expect(h.pushed.at(-1)?.name).toBeNull();
		expect(h.providerCalls.state.clear).toBeGreaterThan(0);
	});

	it("routes startDebugging() === false through cleanup('idle')", async () => {
		setStartDebuggingResult(false);
		const h = makeHarness();
		const { startPromise, session } = await startUntilWaitForDebug(h);
		session.resolveDebug(4711);
		await startPromise;
		expect(h.pushed.at(-1)?.status).toBe("idle");
	});

	it("suppresses showStartFailure when fatal_error preceded the rejection", async () => {
		const h = makeHarness();
		const { startPromise, session } = await startUntilWaitForDebug(h);
		session.fire({
			type: "fatal_error",
			code: "JS_ERROR",
			description: "compile failed",
		});
		session.rejectDebug(new Error("CLI exited"));
		await startPromise;
		const messages = getShownMessages();
		// Positive: showProjectionFailed fired from fatal_error handler.
		expect(messages.some((m) => m.message.includes("projection failed"))).toBe(
			true,
		);
		// Negative: showStartFailure suppressed because fatalErrorSeen.
		expect(
			messages.some((m) => m.message.startsWith("Gaffer: CLI exited")),
		).toBe(false);
	});

	it("suppresses showProjectionFault when fatal_error preceded a non-zero exit", async () => {
		// Plan-table row: #fatalErrorSeen suppression at *both* call sites.
		// This covers the running -> exit handler path; the previous test
		// covers the waitForDebug catch path.
		const h = makeHarness();
		const { session } = await startToRunning(h);
		session.fire({
			type: "fatal_error",
			code: "RUNTIME_ERROR",
			description: "boom",
		});
		session.fire({ type: "exit", code: 99 });
		await flushAllMicrotasks();
		const messages = getShownMessages();
		expect(messages.some((m) => m.message.includes("projection failed"))).toBe(
			true,
		);
		expect(messages.some((m) => m.message.includes("projection faulted"))).toBe(
			false,
		);
	});

	it("shows showProjectionFault when fatal_error did NOT precede a non-zero exit", async () => {
		// Negative control for the suppression test above.
		const h = makeHarness();
		const { session } = await startToRunning(h);
		session.fire({ type: "exit", code: 99 });
		await flushAllMicrotasks();
		const messages = getShownMessages();
		expect(messages.some((m) => m.message.includes("projection faulted"))).toBe(
			true,
		);
	});
});

describe("SessionController exit handling", () => {
	it("running -> exit code 0 routes through cleanup('ended') and preserves state", async () => {
		const h = makeHarness();
		const { session } = await startToRunning(h);
		session.fire({ type: "exit", code: 0 });
		await flushAllMicrotasks();
		expect(h.pushed.at(-1)?.status).toBe("ended");
		expect(h.providerCalls.state.markEnded).toBe(1);
		expect(h.providerCalls.state.clear).toBe(0);
		expect(h.providerCalls.status.markEnded).toBe(1);
		expect(h.providerCalls.step.clear).toBe(1);
	});

	it("starting -> exit routes through cleanup('idle')", async () => {
		const h = makeHarness();
		const { startPromise, session } = await startUntilWaitForDebug(h);
		session.fire({ type: "exit", code: 1 });
		// rejectDebug because process exit while waiting; harness aborts.
		session.rejectDebug(new Error("exited"));
		await startPromise;
		expect(h.pushed.at(-1)?.status).toBe("idle");
	});

	it("late exit from a previous session is ignored", async () => {
		const h = makeHarness();
		const { session: first } = await startToRunning(h);
		first.fire({ type: "exit", code: 0 });
		await flushAllMicrotasks();
		const { session: second } = await startToRunning(h);

		// Fire exit on the first (now-orphaned) session - controller
		// should not react because activeSession !== first.
		const before = h.pushed.length;
		first.fire({ type: "exit", code: 99 });
		await flushAllMicrotasks();
		expect(h.pushed.length).toBe(before);
		expect(h.pushed.at(-1)?.status).toBe("running");
		expect(second).not.toBe(first);
	});
});

describe("SessionController stop", () => {
	it("stop during starting -> cleanup('idle')", async () => {
		const h = makeHarness();
		const { startPromise, session } = await startUntilWaitForDebug(h);
		const stopPromise = h.controller.stop();
		// Resolve the pending waitForDebug so start() unwinds cleanly.
		session.rejectDebug(new Error("aborted"));
		await Promise.all([startPromise, stopPromise]);
		expect(h.pushed.at(-1)?.status).toBe("idle");
	});

	it("stop during running -> cleanup('ended')", async () => {
		const h = makeHarness();
		await startToRunning(h);
		await h.controller.stop();
		expect(h.pushed.at(-1)?.status).toBe("ended");
		expect(h.providerCalls.state.markEnded).toBe(1);
	});

	it("stop is a no-op when idle", async () => {
		const h = makeHarness();
		await h.controller.stop();
		expect(h.pushed).toEqual([]);
		expect(getState().stopDebuggingCount).toBe(0);
	});
});

describe("SessionController cleanup idempotency", () => {
	it("overlapping exit + onDidTerminateDebugSession produce one cleanup", async () => {
		const h = makeHarness();
		const { session, debugSession } = await startToRunning(h);
		// Both events fire in the same tick.
		session.fire({ type: "exit", code: 0 });
		fireDebugTerminated(
			debugSession as unknown as Parameters<typeof fireDebugTerminated>[0],
		);
		await flushAllMicrotasks();
		// Status reaches 'ended' exactly once. Two calls would double-push.
		const endedPushes = h.pushed.filter((s) => s.status === "ended");
		expect(endedPushes).toHaveLength(1);
		expect(h.providerCalls.state.markEnded).toBe(1);
		expect(h.providerCalls.status.markEnded).toBe(1);
		expect(h.providerCalls.step.clear).toBe(1);
	});

	it("terminate of a different debug session is ignored", async () => {
		const h = makeHarness();
		await startToRunning(h);
		const before = h.pushed.length;
		// Different session reference.
		fireDebugTerminated({
			id: "other",
			type: "gaffer",
			configuration: {},
		} as never);
		await flushAllMicrotasks();
		expect(h.pushed.length).toBe(before);
		// And no cleanup side-effects ran for the unrelated session.
		expect(h.providerCalls.state.markEnded).toBe(0);
		expect(h.providerCalls.status.markEnded).toBe(0);
	});
});

describe("SessionController.setEngineMode", () => {
	it("queues mode during starting and applies on transition to running", async () => {
		const h = makeHarness();
		const { startPromise, session } = await startUntilWaitForDebug(h);
		await h.controller.setEngineMode("inspecting");
		session.resolveDebug(4711);
		await startPromise;
		// Initial transition should land directly on inspecting, not running.
		expect(h.pushed.map((s) => s.status)).toEqual(["starting", "inspecting"]);
		expect(h.contextCalls).toEqual([undefined, "inspecting"]);
	});

	it("changes status when called during running", async () => {
		const h = makeHarness();
		await startToRunning(h);
		await h.controller.setEngineMode("inspecting");
		expect(h.pushed.at(-1)?.status).toBe("inspecting");
		expect(h.contextCalls.at(-1)).toBe("inspecting");
	});

	it("is a no-op when already in the requested mode", async () => {
		const h = makeHarness();
		await startToRunning(h);
		const before = h.pushed.length;
		await h.controller.setEngineMode("running");
		expect(h.pushed.length).toBe(before);
	});

	it("is dropped when called outside running/inspecting/starting", async () => {
		const h = makeHarness();
		await h.controller.setEngineMode("inspecting");
		expect(h.pushed).toEqual([]);
	});
});

describe("SessionController.dispose", () => {
	it("disposes the active session and clears the debug session reference", async () => {
		const h = makeHarness();
		const { session } = await startToRunning(h);
		expect(session.disposeCount).toBe(0);
		h.controller.dispose();
		expect(session.disposeCount).toBe(1);
		// A subsequent terminate of the original DAP session must not
		// trigger any cleanup - the controller has cleared its reference.
		const before = h.pushed.length;
		fireDebugTerminated(
			getLastStartedDebugSession() as unknown as Parameters<
				typeof fireDebugTerminated
			>[0],
		);
		await flushAllMicrotasks();
		expect(h.pushed.length).toBe(before);
	});

	it("is safe to call when idle", () => {
		const h = makeHarness();
		expect(() => h.controller.dispose()).not.toThrow();
	});
});

describe("SessionController gaffer.mode setContext across all transitions", () => {
	it("idle->starting->running->ended->idle pushes the right context values", async () => {
		// Plan row: gaffer.mode setContext fires with the right value at
		// each transition. Cover the full cycle in one pass.
		const h = makeHarness();
		const { session } = await startToRunning(h);
		session.fire({ type: "exit", code: 0 });
		await flushAllMicrotasks();
		// running -> ended
		expect(h.contextCalls.at(-1)).toBe("ended");
		// Restart from ended -> cleanup("idle") sets undefined, then
		// starting also undefined, then running.
		await startToRunning(h);
		expect(h.contextCalls).toEqual([
			undefined, // starting (1)
			"running", // running (1)
			"ended", // exit
			undefined, // cleanup("idle") on next start
			undefined, // starting (2)
			"running", // running (2)
		]);
	});

	it("flips between running and inspecting", async () => {
		const h = makeHarness();
		await startToRunning(h);
		await h.controller.setEngineMode("inspecting");
		await h.controller.setEngineMode("running");
		expect(h.contextCalls.slice(-3)).toEqual([
			"running",
			"inspecting",
			"running",
		]);
	});
});

describe("SessionController diagnostic lifecycle across cleanups", () => {
	// Plan-table row: "diagnostics survive across cleanups; cleared on
	// next session." The first half (a fatal_error squiggle survives a
	// running -> ended cleanup) was previously implicit. The second half
	// (next start() clears) had partial coverage. Drive both halves
	// explicitly here.

	it("a fatal-error diagnostic survives running->ended cleanup and is cleared by the next start", async () => {
		const { initDiagnostics } = await import("../diagnostics.js");
		initDiagnostics(makeContext());

		const h = makeHarness();
		const { session } = await startToRunning(h);
		// fatal_error WITH file -> reportFatalError attaches a diagnostic
		// on the gaffer collection.
		session.fire({
			type: "fatal_error",
			code: "JS_ERROR",
			description: "boom",
			file: "/p/checkout/projection.js",
			line: 5,
			column: 3,
		});
		// Non-zero exit routes through cleanup("ended"). Per the source
		// comment in cleanupSession, diagnostics are NOT cleared in
		// cleanup so the squiggle survives for post-mortem inspection.
		session.fire({ type: "exit", code: 1 });
		await flushAllMicrotasks();
		expect(h.pushed.at(-1)?.status).toBe("ended");
		expect(getState().diagnosticCollections[0]?.entries.size).toBe(1);

		// Next start() clears diagnostics before the new session begins.
		await startToRunning(h);
		expect(getState().diagnosticCollections[0]?.entries.size).toBe(0);
	});
});

describe("SessionController onDidStartDebugSession capture guard", () => {
	// Production filters by `s.configuration.type === "gaffer"` AND
	// `status === "starting"`. Without the type guard, a stranger debug
	// session (a Node debug session running in the same window) would
	// be captured and torn down when the user stops it - tearing down
	// our gaffer session along with it.

	it("does not capture a non-gaffer DebugSession started during 'starting'", async () => {
		const h = makeHarness();
		const { startPromise, session } = await startUntilWaitForDebug(h);

		// A foreign debug session starts while we're in `starting`.
		// Production must NOT capture it as #activeDebugSession.
		const foreign = {
			id: "foreign-1",
			type: "node",
			name: "Some Node Debug",
			configuration: { type: "node" },
			customRequest: () => Promise.resolve(undefined),
		} as unknown as vscode.DebugSession;
		fireDebugStarted(foreign);

		// Complete our start cycle - mock auto-fires our own gaffer
		// onDidStartDebugSession during startDebugging.
		session.resolveDebug(4711);
		await startPromise;

		// Tear down the foreign session: must NOT cleanup our gaffer
		// controller, since #activeDebugSession is the gaffer one.
		const before = h.pushed.length;
		fireDebugTerminated(foreign);
		await flushAllMicrotasks();
		expect(h.pushed.length).toBe(before);
		expect(h.pushed.at(-1)?.status).toBe("running");
		// Stronger: no cleanup side-effects ran.
		expect(h.providerCalls.state.markEnded).toBe(0);
		expect(h.providerCalls.status.markEnded).toBe(0);

		// Sanity: terminating OUR session does cleanup.
		const ours = getLastStartedDebugSession();
		if (!ours) throw new Error("expected our debug session to exist");
		fireDebugTerminated(ours);
		await flushAllMicrotasks();
		expect(h.pushed.at(-1)?.status).toBe("ended");
	});

	it("does not capture a gaffer DebugSession that arrives outside the 'starting' window", async () => {
		// While idle, a stale onDidStartDebugSession (e.g. from a stop
		// race where the host fires the event after our cleanup) must
		// not get captured.
		const h = makeHarness();
		const stale = {
			id: "stale",
			type: "gaffer",
			name: "Gaffer: stale",
			configuration: { type: "gaffer" },
			customRequest: () => Promise.resolve(undefined),
		} as unknown as vscode.DebugSession;
		fireDebugStarted(stale);
		// Now do a real start.
		await startToRunning(h);
		// Tear down the stale session: must not cleanup our running session.
		const before = h.pushed.length;
		fireDebugTerminated(stale);
		await flushAllMicrotasks();
		expect(h.pushed.length).toBe(before);
		expect(h.pushed.at(-1)?.status).toBe("running");
		expect(h.providerCalls.state.markEnded).toBe(0);
		expect(h.providerCalls.status.markEnded).toBe(0);
	});
});
