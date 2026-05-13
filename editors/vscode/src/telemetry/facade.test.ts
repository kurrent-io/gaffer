import type { Envelope, ExtensionActivated } from "@kurrent/gaffer-telemetry";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { load, save } from "./config.js";
import { createTelemetry } from "./facade.js";

const activatedEvent: ExtensionActivated = {
	name: "extension_activated",
	timestamp: "2026-05-13T10:00:00.000Z",
	properties: {
		editor: "vscode",
		editor_version: "1.95.2",
		cli_reachable: true,
		cli_version: "0.4",
		activation_duration_ms: 100,
	},
};

let dir: string;

beforeEach(() => {
	dir = fs.mkdtempSync(path.join(os.tmpdir(), "gaffer-telemetry-facade-"));
});
afterEach(() => {
	fs.rmSync(dir, { recursive: true, force: true });
});

function permissiveOpts(extras: { fetchImpl?: typeof fetch } = {}) {
	return {
		storageDir: dir,
		libVersion: "0.4.2",
		env: {} as NodeJS.ProcessEnv,
		vscodeTelemetryLevel: "all",
		log: () => {},
		sinkUrl: "https://telemetry.test.invalid/v1/ingest",
		...(extras.fetchImpl !== undefined && { fetchImpl: extras.fetchImpl }),
	};
}

describe("createTelemetry", () => {
	it("on opt-out: emit is a no-op and never POSTs", async () => {
		const fetchImpl = vi.fn<typeof fetch>(
			async () => new Response("", { status: 200 }),
		);
		const telemetry = await createTelemetry({
			...permissiveOpts({ fetchImpl }),
			env: { DO_NOT_TRACK: "1" } as NodeJS.ProcessEnv,
		});
		telemetry.emit(activatedEvent);
		await telemetry.drain(1000);
		expect(fetchImpl).not.toHaveBeenCalled();
	});

	it("on opt-out: doesn't mint an identity (no file written)", async () => {
		const telemetry = await createTelemetry({
			...permissiveOpts(),
			env: { DO_NOT_TRACK: "1" } as NodeJS.ProcessEnv,
		});
		telemetry.emit(activatedEvent);
		await telemetry.drain(1000);
		// telemetry.json may not exist; if it does, it must not contain
		// a freshly-minted telemetry_id.
		const persisted = await load(dir);
		expect(persisted.telemetry_id).toBeUndefined();
	});

	it("on opt-out via VS Code telemetry.telemetryLevel: emit is a no-op", async () => {
		const fetchImpl = vi.fn<typeof fetch>(async () => new Response(""));
		const telemetry = await createTelemetry({
			...permissiveOpts({ fetchImpl }),
			vscodeTelemetryLevel: "off",
		});
		telemetry.emit(activatedEvent);
		await telemetry.drain(1000);
		expect(fetchImpl).not.toHaveBeenCalled();
	});

	it("drain on the no-op handle resolves immediately", async () => {
		const telemetry = await createTelemetry({
			...permissiveOpts(),
			env: { DO_NOT_TRACK: "1" } as NodeJS.ProcessEnv,
		});
		const start = Date.now();
		await telemetry.drain(5000);
		expect(Date.now() - start).toBeLessThan(100);
	});

	it("mints + persists an identity on first activation", async () => {
		const telemetry = await createTelemetry(
			permissiveOpts({ fetchImpl: vi.fn(async () => new Response("")) }),
		);
		telemetry.emit(activatedEvent);
		await telemetry.drain(1000);

		const persisted = await load(dir);
		expect(persisted.telemetry_id).toMatch(/^[0-9a-f-]{36}$/);
	});

	it("invokerId() returns the per-install telemetry_id on a live handle", async () => {
		const seeded = {
			telemetry_id: "8f2b1a4c-9e7d-4a3e-b5f2-7c8a9d4e1f02",
		};
		await save(dir, seeded);
		const telemetry = await createTelemetry(permissiveOpts());
		expect(telemetry.invokerId()).toBe(seeded.telemetry_id);
	});

	it("invokerId() returns null when opted out (no identity to share)", async () => {
		const telemetry = await createTelemetry({
			...permissiveOpts(),
			env: { DO_NOT_TRACK: "1" } as NodeJS.ProcessEnv,
		});
		expect(telemetry.invokerId()).toBeNull();
	});

	it("isOptedOut() is true when opted out at construction", async () => {
		const telemetry = await createTelemetry({
			...permissiveOpts(),
			env: { DO_NOT_TRACK: "1" } as NodeJS.ProcessEnv,
		});
		expect(telemetry.isOptedOut()).toBe(true);
	});

	it("isOptedOut() is false on a live, consenting handle", async () => {
		const telemetry = await createTelemetry(permissiveOpts());
		expect(telemetry.isOptedOut()).toBe(false);
	});

	it("isOptedOut() flips true after refreshOptOut latches disabled", async () => {
		const telemetry = await createTelemetry(permissiveOpts());
		expect(telemetry.isOptedOut()).toBe(false);
		const cur = await load(dir);
		await save(dir, { ...cur, telemetry_enabled: false, disclosed: true });
		await telemetry.refreshOptOut();
		expect(telemetry.isOptedOut()).toBe(true);
	});

	it("invokerId() returns null after a mid-session refreshOptOut latches disabled", async () => {
		const telemetry = await createTelemetry(permissiveOpts());
		expect(telemetry.invokerId()).not.toBeNull();
		const cur = await load(dir);
		await save(dir, { ...cur, telemetry_enabled: false, disclosed: true });
		await telemetry.refreshOptOut();
		expect(telemetry.invokerId()).toBeNull();
	});

	it("reuses a persisted identity on subsequent activations", async () => {
		const seeded = {
			telemetry_id: "8f2b1a4c-9e7d-4a3e-b5f2-7c8a9d4e1f02",
		};
		await save(dir, seeded);

		const fetchImpl = vi.fn<typeof fetch>(async () => new Response(""));
		const telemetry = await createTelemetry(permissiveOpts({ fetchImpl }));
		telemetry.emit(activatedEvent);
		await telemetry.drain(1000);

		const call = fetchImpl.mock.calls[0];
		if (call === undefined) throw new Error("expected a fetch call");
		const init = call[1];
		if (init === undefined) throw new Error("expected fetch init arg");
		const body = JSON.parse(init.body as string) as Envelope;
		expect(body.emitter_id).toBe(seeded.telemetry_id);
	});

	it("posts an envelope wrapping the event with the resolved identity", async () => {
		const fetchImpl = vi.fn<typeof fetch>(async () => new Response(""));
		const telemetry = await createTelemetry(permissiveOpts({ fetchImpl }));
		telemetry.emit(activatedEvent);
		await telemetry.drain(1000);

		expect(fetchImpl).toHaveBeenCalledTimes(1);
		const call = fetchImpl.mock.calls[0];
		if (call === undefined) throw new Error("expected one fetch call");
		const [url, init] = call;
		expect(url).toBe("https://telemetry.test.invalid/v1/ingest");
		if (init === undefined) throw new Error("expected fetch init arg");
		const body = JSON.parse(init.body as string) as Envelope;
		expect(body.schema_version).toBe("1");
		expect(body.context.emitter).toBe("vscode");
		expect(body.context.lib_version).toBe("0.4.2");
		expect(body.events).toEqual([activatedEvent]);
	});

	it("falls back to no-op on an I/O failure during init (doesn't crash activation)", async () => {
		// Force loadSafe -> readFile to fail with EACCES by replacing
		// the storage dir with an unreadable file. createTelemetry
		// must catch and return a working no-op handle.
		if (process.platform === "win32" || process.getuid?.() === 0) {
			return;
		}
		const file = path.join(dir, "telemetry.json");
		fs.writeFileSync(file, "{}");
		fs.chmodSync(file, 0o000);
		try {
			const log = vi.fn();
			const fetchImpl = vi.fn<typeof fetch>(async () => new Response(""));
			const telemetry = await createTelemetry({
				...permissiveOpts({ fetchImpl }),
				log,
			});
			expect(() => telemetry.emit(activatedEvent)).not.toThrow();
			await telemetry.drain(1000);
			expect(fetchImpl).not.toHaveBeenCalled();
			expect(log).toHaveBeenCalled();
			// Init failure is not the same as opt-out: the user hasn't
			// chosen to opt out, so spawn-side propagation MUST NOT
			// silence the CLI on this path.
			expect(telemetry.isOptedOut()).toBe(false);
			expect(telemetry.invokerId()).toBeNull();
		} finally {
			fs.chmodSync(file, 0o600);
		}
	});

	it("refreshOptOut: late `[Disable]` write silences subsequent emits", async () => {
		const fetchImpl = vi.fn<typeof fetch>(async () => new Response(""));
		const telemetry = await createTelemetry(permissiveOpts({ fetchImpl }));
		telemetry.emit(activatedEvent);
		await telemetry.drain(1000);
		expect(fetchImpl).toHaveBeenCalledTimes(1);

		// User clicks `[Disable]`; runFirstRunNotice writes false.
		const cur = await load(dir);
		await save(dir, {
			...cur,
			telemetry_enabled: false,
			disclosed: true,
		});
		await telemetry.refreshOptOut();

		telemetry.emit(activatedEvent);
		await telemetry.drain(1000);
		expect(fetchImpl).toHaveBeenCalledTimes(1);
	});

	it("refreshOptOut is one-way: re-enabling mid-session stays disabled", async () => {
		const fetchImpl = vi.fn<typeof fetch>(async () => new Response(""));
		const telemetry = await createTelemetry(permissiveOpts({ fetchImpl }));
		const cur = await load(dir);
		await save(dir, { ...cur, telemetry_enabled: false, disclosed: true });
		await telemetry.refreshOptOut();
		// Flip back to true on disk - latch should ignore.
		await save(dir, { ...cur, telemetry_enabled: true, disclosed: true });
		await telemetry.refreshOptOut();

		telemetry.emit(activatedEvent);
		await telemetry.drain(1000);
		expect(fetchImpl).not.toHaveBeenCalled();
	});

	it("drains in-flight sends on demand", async () => {
		let release!: () => void;
		const blocked = new Promise<Response>((r) => {
			release = () => r(new Response(""));
		});
		const fetchImpl = vi.fn<typeof fetch>(async () => blocked);
		const telemetry = await createTelemetry(permissiveOpts({ fetchImpl }));

		telemetry.emit(activatedEvent);
		const drainPromise = telemetry.drain(2000);
		let settled = false;
		void drainPromise.then(() => {
			settled = true;
		});
		await new Promise((r) => setTimeout(r, 20));
		expect(settled).toBe(false);

		release();
		await drainPromise;
		expect(settled).toBe(true);
	});
});

describe("Telemetry.reportException", () => {
	it("builds an exception envelope and ships it through the sink", async () => {
		const fetchImpl = vi.fn<typeof fetch>(async () => new Response(""));
		const telemetry = await createTelemetry({
			...permissiveOpts({ fetchImpl }),
			extensionPath: "/opt/gaffer",
			getWorkspaceFolders: () => [],
		});
		const err = new Error("kaboom");
		err.stack =
			"Error: kaboom\n    at activate (/opt/gaffer/dist/extension.js:42:13)";
		telemetry.reportException("startup", err);
		await telemetry.drain(1000);

		expect(fetchImpl).toHaveBeenCalledTimes(1);
		const call = fetchImpl.mock.calls[0];
		if (call === undefined) throw new Error("expected a fetch call");
		const body = JSON.parse(call[1]?.body as string) as Envelope;
		expect(body.events[0]?.name).toBe("exception");
	});

	it("is a no-op after refreshOptOut latches disabled", async () => {
		const fetchImpl = vi.fn<typeof fetch>(async () => new Response(""));
		const telemetry = await createTelemetry({
			...permissiveOpts({ fetchImpl }),
			extensionPath: "/opt/gaffer",
			getWorkspaceFolders: () => [],
		});
		const cur = await load(dir);
		await save(dir, { ...cur, telemetry_enabled: false, disclosed: true });
		await telemetry.refreshOptOut();
		telemetry.reportException("startup", new Error("ignored"));
		await telemetry.drain(1000);
		expect(fetchImpl).not.toHaveBeenCalled();
	});
});
