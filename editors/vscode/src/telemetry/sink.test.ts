import type { Envelope } from "@kurrent/gaffer-telemetry";
import { describe, expect, it, vi } from "vitest";

import { createSink, INGEST_URL } from "./sink.js";

const envelope: Envelope = {
	schema_version: "1",
	emitter_id: "8f2b1a4c-9e7d-4a3e-b5f2-7c8a9d4e1f02",
	run_id: "33333333-3333-3333-3333-333333333333",
	context: {
		emitter: "vscode",
		lib_version: "0.4.2",
		os: "linux",
		arch: "x64",
		runtime_environment: "local",
	},
	events: [],
};

function makeFetch(
	impl: typeof fetch = async () => new Response("", { status: 200 }),
) {
	return vi.fn(impl);
}

describe("createSink.send", () => {
	it("POSTs to the configured url with the envelope as JSON", async () => {
		const fetchImpl = makeFetch();
		const sink = createSink({
			url: INGEST_URL,
			debug: false,
			log: () => {},
			fetchImpl,
		});

		sink.send(envelope);
		await sink.drain(1000);

		expect(fetchImpl).toHaveBeenCalledTimes(1);
		const call = fetchImpl.mock.calls[0];
		if (call === undefined) throw new Error("expected one fetch call");
		const [url, init] = call;
		expect(url).toBe(INGEST_URL);
		if (init === undefined) throw new Error("expected fetch init arg");
		expect(init).toMatchObject({
			method: "POST",
			headers: { "content-type": "application/json" },
		});
		expect(JSON.parse(init.body as string)).toEqual(envelope);
	});

	it("does not throw or await when fetch rejects (drop-on-failure)", async () => {
		const fetchImpl = makeFetch(async () => {
			throw new Error("network unreachable");
		});
		const sink = createSink({
			url: INGEST_URL,
			debug: false,
			log: () => {},
			fetchImpl,
		});

		// send must not throw; drain must settle silently.
		expect(() => sink.send(envelope)).not.toThrow();
		await expect(sink.drain(1000)).resolves.toBeUndefined();
	});

	it("does not throw on non-2xx responses (worker is 200-only)", async () => {
		const fetchImpl = makeFetch(async () => new Response("", { status: 500 }));
		const sink = createSink({
			url: INGEST_URL,
			debug: false,
			log: () => {},
			fetchImpl,
		});

		expect(() => sink.send(envelope)).not.toThrow();
		await expect(sink.drain(1000)).resolves.toBeUndefined();
	});

	it("logs the envelope when debug=true, not when false", () => {
		const log = vi.fn();
		const sink = createSink({
			url: INGEST_URL,
			debug: true,
			log,
			fetchImpl: makeFetch(),
		});

		sink.send(envelope);

		expect(log).toHaveBeenCalledTimes(1);
		const logCall = log.mock.calls[0];
		if (logCall === undefined) throw new Error("expected one log call");
		const [line] = logCall;
		expect(line).toContain("gaffer-telemetry:");
		expect(line).toContain(envelope.emitter_id);

		const quietLog = vi.fn();
		const quiet = createSink({
			url: INGEST_URL,
			debug: false,
			log: quietLog,
			fetchImpl: makeFetch(),
		});
		quiet.send(envelope);
		expect(quietLog).not.toHaveBeenCalled();
	});
});

describe("createSink.drain", () => {
	it("returns immediately when nothing is pending", async () => {
		const sink = createSink({
			url: INGEST_URL,
			debug: false,
			log: () => {},
			fetchImpl: makeFetch(),
		});
		const start = Date.now();
		await sink.drain(5000);
		expect(Date.now() - start).toBeLessThan(100);
	});

	it("waits for in-flight sends to settle", async () => {
		let release!: () => void;
		const blocked = new Promise<Response>((resolve) => {
			release = () => resolve(new Response("", { status: 200 }));
		});
		const fetchImpl = vi.fn(async () => blocked);
		const sink = createSink({
			url: INGEST_URL,
			debug: false,
			log: () => {},
			fetchImpl,
		});

		sink.send(envelope);
		// drain shouldn't resolve before fetch resolves.
		let drained = false;
		const drainPromise = sink.drain(2000).then(() => {
			drained = true;
		});
		await new Promise((r) => setTimeout(r, 20));
		expect(drained).toBe(false);

		release();
		await drainPromise;
		expect(drained).toBe(true);
	});

	it("clears the timeout timer when settle wins (doesn't keep the event loop alive)", async () => {
		const fetchImpl = makeFetch();
		const sink = createSink({
			url: INGEST_URL,
			debug: false,
			log: () => {},
			fetchImpl,
		});

		const before = process.getActiveResourcesInfo?.() ?? [];
		sink.send(envelope);
		await sink.drain(60_000); // generous timeout we should never hit
		const after = process.getActiveResourcesInfo?.() ?? [];

		// No new Timeout resources should be hanging around after drain
		// returns - the early-settle path must clearTimeout the racer.
		const newTimeouts =
			after.filter((r) => r === "Timeout").length -
			before.filter((r) => r === "Timeout").length;
		expect(newTimeouts).toBe(0);
	});

	it("times out when sends don't settle within the grace period", async () => {
		// fetch never resolves - drain must still return within the budget.
		const fetchImpl = vi.fn(async () => new Promise<Response>(() => {}));
		const sink = createSink({
			url: INGEST_URL,
			debug: false,
			log: () => {},
			fetchImpl,
		});

		sink.send(envelope);
		const start = Date.now();
		await sink.drain(50);
		const elapsed = Date.now() - start;
		expect(elapsed).toBeGreaterThanOrEqual(40);
		expect(elapsed).toBeLessThan(500);
	});
});
