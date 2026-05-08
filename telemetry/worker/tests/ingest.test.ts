import { exports } from "cloudflare:workers";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const validEnvelope = {
	schema_version: "1",
	emitter_id: "00000000-0000-0000-0000-000000000001",
	run_id: "00000000-0000-0000-0000-000000000002",
	context: {
		emitter: "cli",
		lib_version: "0.4.2",
		os: "linux",
		arch: "x64",
		runtime_environment: "local",
	},
	events: [
		{
			name: "command_invoked",
			timestamp: "2026-05-08T12:00:00.000Z",
			properties: {
				command: "version",
				duration_ms: 10,
				outcome: "success",
				invoked_by: "direct",
				invoked_via: "terminal",
			},
		},
	],
};

const worker = exports.default;

// Stub global fetch with a vitest mock so we can observe and control what the
// worker tries to send to PostHog. Default behaviour is "PostHog accepted it";
// individual tests override per-test.
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
	fetchMock = vi.fn(async () => new Response("ok", { status: 200 }));
	vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
	vi.unstubAllGlobals();
});

describe("POST /v1/ingest", () => {
	it("returns 200 for a valid envelope", async () => {
		const res = await worker.fetch(
			new Request("https://example.com/v1/ingest", {
				method: "POST",
				body: JSON.stringify(validEnvelope),
			}),
		);
		expect(res.status).toBe(200);
	});

	it("returns 200 (drops) for invalid JSON", async () => {
		const res = await worker.fetch(
			new Request("https://example.com/v1/ingest", {
				method: "POST",
				body: "{not json",
			}),
		);
		expect(res.status).toBe(200);
		expect(fetchMock).not.toHaveBeenCalled();
	});

	it("returns 200 (drops) for an envelope that fails schema validation", async () => {
		const bad = { ...validEnvelope, emitter_id: "not-a-uuid" };
		const res = await worker.fetch(
			new Request("https://example.com/v1/ingest", {
				method: "POST",
				body: JSON.stringify(bad),
			}),
		);
		expect(res.status).toBe(200);
		expect(fetchMock).not.toHaveBeenCalled();
	});

	it("returns 200 even when PostHog is unreachable", async () => {
		fetchMock.mockRejectedValueOnce(new Error("network unreachable"));
		const res = await worker.fetch(
			new Request("https://example.com/v1/ingest", {
				method: "POST",
				body: JSON.stringify(validEnvelope),
			}),
		);
		expect(res.status).toBe(200);
	});

	it("forwards to PostHog with the api_key in the body", async () => {
		await worker.fetch(
			new Request("https://example.com/v1/ingest", {
				method: "POST",
				body: JSON.stringify(validEnvelope),
			}),
		);

		// Wait for the waitUntil-deferred PostHog call to actually fire.
		await vi.waitFor(() => expect(fetchMock).toHaveBeenCalled());

		const [url, init] = fetchMock.mock.calls[0]!;
		expect(url).toBe("https://eu.i.posthog.com/batch");
		expect(init?.method).toBe("POST");
		const body = JSON.parse(init?.body as string);
		expect(body).toMatchObject({
			api_key: expect.any(String),
			batch: [{ event: "command_invoked", distinct_id: validEnvelope.emitter_id }],
		});
	});
});

describe("GET /", () => {
	it("returns the notice HTML", async () => {
		const res = await worker.fetch(new Request("https://example.com/"));
		expect(res.status).toBe(200);
		expect(res.headers.get("content-type")).toContain("text/html");
		const body = await res.text();
		expect(body.toLowerCase()).toContain("<!doctype html>");
		expect(body).toContain("Gaffer telemetry");
	});
});

describe("unmatched routes", () => {
	it("returns 404 for an unknown path", async () => {
		const res = await worker.fetch(new Request("https://example.com/unknown"));
		expect(res.status).toBe(404);
	});

	it("returns 404 for GET /v1/ingest (POST only)", async () => {
		const res = await worker.fetch(new Request("https://example.com/v1/ingest"));
		expect(res.status).toBe(404);
	});
});
