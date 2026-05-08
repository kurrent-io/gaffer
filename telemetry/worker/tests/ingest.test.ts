import { fetchMock, SELF } from "cloudflare:test";
import { afterAll, beforeAll, beforeEach, describe, expect, it } from "vitest";

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

beforeAll(() => {
	fetchMock.activate();
	fetchMock.disableNetConnect();
});
afterAll(() => fetchMock.deactivate());
beforeEach(() => fetchMock.assertNoPendingInterceptors());

describe("POST /v1/ingest", () => {
	it("returns 200 for a valid envelope", async () => {
		fetchMock.get("https://eu.i.posthog.com").intercept({ path: "/batch", method: "POST" }).reply(200, "ok");

		const res = await SELF.fetch("https://example.com/v1/ingest", {
			method: "POST",
			body: JSON.stringify(validEnvelope),
		});
		expect(res.status).toBe(200);
	});

	it("returns 200 (drops) for invalid JSON", async () => {
		const res = await SELF.fetch("https://example.com/v1/ingest", {
			method: "POST",
			body: "{not json",
		});
		expect(res.status).toBe(200);
	});

	it("returns 200 (drops) for an envelope that fails schema validation", async () => {
		const bad = { ...validEnvelope, emitter_id: "not-a-uuid" };
		const res = await SELF.fetch("https://example.com/v1/ingest", {
			method: "POST",
			body: JSON.stringify(bad),
		});
		expect(res.status).toBe(200);
	});

	it("returns 200 even when PostHog is unreachable", async () => {
		fetchMock
			.get("https://eu.i.posthog.com")
			.intercept({ path: "/batch", method: "POST" })
			.replyWithError(new Error("network unreachable"));

		const res = await SELF.fetch("https://example.com/v1/ingest", {
			method: "POST",
			body: JSON.stringify(validEnvelope),
		});
		expect(res.status).toBe(200);
	});

	it("forwards to PostHog with the api_key in the body", async () => {
		let receivedBody: unknown;
		fetchMock
			.get("https://eu.i.posthog.com")
			.intercept({ path: "/batch", method: "POST" })
			.reply((opts) => {
				receivedBody = JSON.parse(opts.body as string);
				return { statusCode: 200, data: "ok" };
			});

		await SELF.fetch("https://example.com/v1/ingest", {
			method: "POST",
			body: JSON.stringify(validEnvelope),
		});

		// Wait for the waitUntil-deferred PostHog call to actually fire.
		await new Promise((r) => setTimeout(r, 50));

		expect(receivedBody).toMatchObject({
			api_key: expect.any(String),
			batch: [{ event: "command_invoked", distinct_id: validEnvelope.emitter_id }],
		});
	});
});

describe("GET /", () => {
	it("returns the notice HTML", async () => {
		const res = await SELF.fetch("https://example.com/");
		expect(res.status).toBe(200);
		expect(res.headers.get("content-type")).toContain("text/html");
		const body = await res.text();
		expect(body.toLowerCase()).toContain("<!doctype html>");
		expect(body).toContain("Gaffer telemetry");
	});
});

describe("unmatched routes", () => {
	it("returns 404 for an unknown path", async () => {
		const res = await SELF.fetch("https://example.com/unknown");
		expect(res.status).toBe(404);
	});

	it("returns 404 for GET /v1/ingest (POST only)", async () => {
		const res = await SELF.fetch("https://example.com/v1/ingest");
		expect(res.status).toBe(404);
	});
});
