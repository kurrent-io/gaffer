// Integration tests against a real subprocess. Spawning a tiny `node -e
// ...` child catches the bugs that motivated the plan's "don't fake
// child_process" rule: line buffering at OS boundaries, exit ordering,
// process-group kill on POSIX. Each test waits for the harness to
// settle before asserting.

import { describe, expect, it } from "vitest";
import type { CliMessage } from "./schemas.js";
import { GafferProcess } from "./process.js";

function nodeArgv(script: string): string[] {
	return [process.execPath, "-e", script];
}

function recordMessages(proc: GafferProcess): {
	messages: CliMessage[];
	exitCode: { code: number | null } | null;
	exited: Promise<void>;
} {
	const messages: CliMessage[] = [];
	let resolve: () => void;
	const exited = new Promise<void>((r) => {
		resolve = r;
	});
	const result: {
		messages: CliMessage[];
		exitCode: { code: number | null } | null;
		exited: Promise<void>;
	} = { messages, exitCode: null, exited };
	proc.onLine((m) => messages.push(m));
	proc.onExit((code) => {
		result.exitCode = { code };
		resolve();
	});
	return result;
}

describe("GafferProcess", () => {
	it("rejects an empty argv at construction", () => {
		expect(() => new GafferProcess([])).toThrow(/argv must not be empty/);
	});

	it("parses NDJSON lines into typed CliMessage values", async () => {
		const script = `
			console.log(JSON.stringify({type: "info", projection: {name: "checkout"}}));
			console.log(JSON.stringify({type: "event", sequenceNumber: 1, streamId: "s", eventType: "T"}));
			console.log(JSON.stringify({type: "summary", handled: 1, skipped: 0, errors: 0}));
		`;
		const proc = new GafferProcess(nodeArgv(script));
		const rec = recordMessages(proc);
		proc.start();
		await rec.exited;
		expect(rec.messages.map((m) => m.type)).toEqual([
			"info",
			"event",
			"summary",
		]);
		expect(rec.exitCode?.code).toBe(0);
	});

	it("dispatches malformed lines as logs (no message dispatched)", async () => {
		// Plain stdout: `not json` -> falls through to log("[stdout] ...")
		// rather than dispatching.
		const script = `
			process.stdout.write("not json\\n");
			console.log(JSON.stringify({type: "summary", handled: 0, skipped: 0, errors: 0}));
		`;
		const proc = new GafferProcess(nodeArgv(script));
		const rec = recordMessages(proc);
		proc.start();
		await rec.exited;
		expect(rec.messages.map((m) => m.type)).toEqual(["summary"]);
	});

	it("dispatches a synthetic exit message with the child's exit code", async () => {
		const script = `process.exit(7);`;
		const proc = new GafferProcess(nodeArgv(script));
		const rec = recordMessages(proc);
		proc.start();
		await rec.exited;
		expect(rec.exitCode?.code).toBe(7);
	});

	it("waitForMessage resolves when the requested type arrives", async () => {
		const script = `
			console.log(JSON.stringify({type: "info", projection: {name: "x"}}));
			console.log(JSON.stringify({type: "debug", port: 4711}));
			setTimeout(() => process.exit(0), 50);
		`;
		const proc = new GafferProcess(nodeArgv(script));
		recordMessages(proc);
		proc.start();
		const debug = await proc.waitForMessage("debug", 5000);
		expect(debug.port).toBe(4711);
	});

	it("waitForMessage rejects if the process exits before the message arrives", async () => {
		const script = `
			console.log(JSON.stringify({type: "info", projection: {name: "x"}}));
			process.exit(0);
		`;
		const proc = new GafferProcess(nodeArgv(script));
		recordMessages(proc);
		proc.start();
		await expect(proc.waitForMessage("debug", 5000)).rejects.toThrow(
			/Process exited/,
		);
	});

	it("waitForMessage times out and kills the child", async () => {
		const script = `
			console.log(JSON.stringify({type: "info", projection: {name: "x"}}));
			setInterval(() => {}, 1000); // keep alive
		`;
		const proc = new GafferProcess(nodeArgv(script));
		const rec = recordMessages(proc);
		proc.start();
		await expect(proc.waitForMessage("debug", 100)).rejects.toThrow(/Timeout/);
		// The timeout calls kill(); the process exits shortly after.
		await rec.exited;
	});

	it("kill terminates a child that is still running", async () => {
		const script = `setInterval(() => {}, 1000); console.log("started");`;
		const proc = new GafferProcess(nodeArgv(script));
		const rec = recordMessages(proc);
		proc.start();
		// Give it a moment to actually start.
		await new Promise((r) => setTimeout(r, 50));
		proc.kill();
		await rec.exited;
		// Either signaled exit (null code) or numeric; both are valid signs
		// the child terminated.
		expect(rec.exitCode).not.toBeNull();
	});

	it("kill is a safe no-op when called before start", () => {
		const proc = new GafferProcess(nodeArgv("process.exit(0)"));
		expect(() => proc.kill()).not.toThrow();
	});

	it("kill is a safe no-op when called after the child has already exited", async () => {
		const proc = new GafferProcess(nodeArgv("process.exit(0)"));
		const rec = recordMessages(proc);
		proc.start();
		await rec.exited;
		expect(() => proc.kill()).not.toThrow();
	});
});
