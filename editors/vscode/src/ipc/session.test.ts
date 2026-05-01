import { beforeEach, describe, expect, it, vi } from "vitest";

// Replace process.ts with a fake before importing GafferSession. The
// fake records what session.ts wires up via onLine/onExit/start/kill so
// we can assert listener fan-out, post-stop detach, and the channel
// header on construction without spawning a child.

const fakeProc = {
	onLineFn: undefined as ((m: unknown) => void) | undefined,
	onExitFn: undefined as ((code: number | null) => void) | undefined,
	startCount: 0,
	killCount: 0,
	waitForMessageImpl: undefined as
		| ((type: string, timeoutMs?: number) => Promise<unknown>)
		| undefined,
};

vi.mock("./process.js", () => {
	class FakeGafferProcess {
		constructor(_argv: string[], _options?: { cwd?: string }) {}
		onLine(fn: (m: unknown) => void): this {
			fakeProc.onLineFn = fn;
			return this;
		}
		onExit(fn: (code: number | null) => void): this {
			fakeProc.onExitFn = fn;
			return this;
		}
		start(): this {
			fakeProc.startCount++;
			return this;
		}
		kill(): void {
			fakeProc.killCount++;
		}
		waitForMessage(type: string, timeoutMs?: number): Promise<unknown> {
			if (fakeProc.waitForMessageImpl) {
				return fakeProc.waitForMessageImpl(type, timeoutMs);
			}
			return Promise.reject(new Error("no waitForMessage impl set"));
		}
	}
	return { GafferProcess: FakeGafferProcess };
});

import { GafferSession } from "./session.js";
import { initOutput } from "../output.js";
import { makeContext } from "../../test/testutil/fake-context.js";
import { getState } from "../../test/testutil/vscode-state.js";
import type { CliMessage } from "./schemas.js";

beforeEach(() => {
	fakeProc.onLineFn = undefined;
	fakeProc.onExitFn = undefined;
	fakeProc.startCount = 0;
	fakeProc.killCount = 0;
	fakeProc.waitForMessageImpl = undefined;
	initOutput(makeContext());
});

describe("GafferSession lifecycle", () => {
	it("clears the output channel and writes a session header on construction", () => {
		new GafferSession("checkout", ["gaffer"]);
		const channel = getState().outputChannels.at(-1);
		expect(channel?.clearCount).toBe(1);
		expect(channel?.lines).toContain("=== checkout ===");
	});

	it("start spawns the underlying process and wires onLine/onExit", () => {
		const session = new GafferSession("checkout", ["gaffer"]);
		session.start();
		expect(fakeProc.startCount).toBe(1);
		expect(fakeProc.onLineFn).toBeDefined();
		expect(fakeProc.onExitFn).toBeDefined();
	});

	it("waitForDebug throws before start", async () => {
		const session = new GafferSession("checkout", ["gaffer"]);
		await expect(session.waitForDebug()).rejects.toThrow(/not started/);
	});
});

describe("GafferSession listener fan-out", () => {
	it("fires per-type listeners for matching messages only", () => {
		const session = new GafferSession("checkout", ["gaffer"]);
		const events: string[] = [];
		session.on("event", () => events.push("event"));
		session.on("result", () => events.push("result"));
		session.start();
		fakeProc.onLineFn?.({
			type: "event",
			sequenceNumber: 1,
			streamId: "s",
			eventType: "T",
		} as CliMessage);
		fakeProc.onLineFn?.({
			type: "result",
			status: "processed",
		} as CliMessage);
		fakeProc.onLineFn?.({
			type: "result",
			status: "skipped",
			reason: "x",
		} as CliMessage);
		expect(events).toEqual(["event", "result", "result"]);
	});

	it("fires '*' listeners for every message type", () => {
		const session = new GafferSession("checkout", ["gaffer"]);
		const types: string[] = [];
		session.on("*", (m) => types.push(m.type));
		session.start();
		fakeProc.onLineFn?.({
			type: "event",
			sequenceNumber: 1,
			streamId: "s",
			eventType: "T",
		} as CliMessage);
		fakeProc.onLineFn?.({
			type: "summary",
			handled: 1,
			skipped: 0,
			errors: 0,
		} as CliMessage);
		expect(types).toEqual(["event", "summary"]);
	});

	it("fires multiple listeners for the same type in registration order", () => {
		const session = new GafferSession("checkout", ["gaffer"]);
		const order: string[] = [];
		session.on("result", () => order.push("a"));
		session.on("result", () => order.push("b"));
		session.start();
		fakeProc.onLineFn?.({
			type: "result",
			status: "processed",
		} as CliMessage);
		expect(order).toEqual(["a", "b"]);
	});

	it("synthesises an exit message via onExit", () => {
		const session = new GafferSession("checkout", ["gaffer"]);
		const exits: Array<number | null> = [];
		session.on("exit", (m) => exits.push(m.code));
		session.start();
		fakeProc.onExitFn?.(0);
		fakeProc.onExitFn?.(7);
		expect(exits).toEqual([0, 7]);
	});
});

describe("GafferSession.stop", () => {
	it("kills the underlying process and detaches handlers", () => {
		const session = new GafferSession("checkout", ["gaffer"]);
		const events: string[] = [];
		session.on("exit", (m) => events.push(`exit:${m.code}`));
		session.start();
		const onLineBefore = fakeProc.onLineFn;
		session.stop();
		expect(fakeProc.killCount).toBe(1);
		// onLine and onExit handlers should have been replaced with no-ops
		// to swallow buffered output between SIGTERM and process exit.
		expect(fakeProc.onLineFn).not.toBe(onLineBefore);
		// Firing the (replaced) post-stop handlers must not invoke
		// caller-registered listeners.
		fakeProc.onExitFn?.(99);
		expect(events).toEqual([]);
	});

	it("is a no-op when the process has already been stopped", () => {
		const session = new GafferSession("checkout", ["gaffer"]);
		session.start();
		session.stop();
		session.stop();
		expect(fakeProc.killCount).toBe(1);
	});

	it("is a no-op when the process was never started", () => {
		const session = new GafferSession("checkout", ["gaffer"]);
		expect(() => session.stop()).not.toThrow();
		expect(fakeProc.killCount).toBe(0);
	});
});

describe("GafferSession.dispose", () => {
	it("stops the process and clears all listeners", () => {
		const session = new GafferSession("checkout", ["gaffer"]);
		const events: string[] = [];
		session.on("event", () => events.push("event"));
		session.start();
		session.dispose();
		expect(fakeProc.killCount).toBe(1);
		// Listeners cleared - even if a stale message snuck in via
		// onLine, no caller listener fires.
		fakeProc.onLineFn?.({
			type: "event",
			sequenceNumber: 1,
			streamId: "s",
			eventType: "T",
		} as CliMessage);
		expect(events).toEqual([]);
	});
});

describe("GafferSession.waitForDebug", () => {
	it("delegates to the process and resolves with the debug message", async () => {
		const session = new GafferSession("checkout", ["gaffer"]);
		fakeProc.waitForMessageImpl = (type) => {
			expect(type).toBe("debug");
			return Promise.resolve({ type: "debug", port: 4711 });
		};
		session.start();
		const msg = await session.waitForDebug();
		expect(msg.port).toBe(4711);
	});
});
