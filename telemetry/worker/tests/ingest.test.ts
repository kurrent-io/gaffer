import { exports } from "cloudflare:workers";
import { createExecutionContext, env, waitOnExecutionContext } from "cloudflare:test";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { handleIngest } from "../src/ingest";
import { applyMigrations, resetTables } from "./migrations";

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

const envelopeWithInvoker = {
	...validEnvelope,
	emitter_id: "00000000-0000-0000-0000-0000000000c1",
	run_id: "00000000-0000-0000-0000-0000000000c2",
	context: {
		...validEnvelope.context,
		invoker_id: "00000000-0000-0000-0000-0000000000ee",
	},
};

const worker = exports.default;

let fetchMock: ReturnType<typeof vi.fn>;

beforeAll(async () => {
	await applyMigrations(env.DB);
});

beforeEach(async () => {
	await resetTables(env.DB);
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

	it("returns 200 (drops) for an oversized body, even without a Content-Length header", async () => {
		// Pad an otherwise-valid envelope past MAX_BODY_BYTES (1 MiB). A stream
		// passed as ReadableStream omits Content-Length, so this would slip
		// through a header-only check.
		const big = { ...validEnvelope, _pad: "x".repeat(1024 * 1024 + 1) };
		const json = JSON.stringify(big);
		const stream = new ReadableStream({
			start(controller) {
				controller.enqueue(new TextEncoder().encode(json));
				controller.close();
			},
		});
		const res = await worker.fetch(
			new Request("https://example.com/v1/ingest", {
				method: "POST",
				body: stream,
				// @ts-expect-error duplex is required by fetch with a stream body
				duplex: "half",
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

	it("forwards to PostHog with the api_key and a stamped session_id", async () => {
		await worker.fetch(
			new Request("https://example.com/v1/ingest", {
				method: "POST",
				body: JSON.stringify(validEnvelope),
			}),
		);

		await vi.waitFor(() => expect(fetchMock).toHaveBeenCalled());

		const [url, init] = fetchMock.mock.calls[0];
		expect(url).toBe("https://eu.i.posthog.com/batch");
		expect(init?.method).toBe("POST");
		const body = JSON.parse(init?.body as string);
		expect(body).toMatchObject({
			api_key: "phc_test_fixture_key",
			batch: [
				{
					event: "command_invoked",
					distinct_id: validEnvelope.emitter_id,
					properties: expect.objectContaining({
						$session_id: expect.stringMatching(/^[0-9a-f-]{36}$/),
					}),
				},
			],
		});
	});

	it("fires $merge_dangerously when the envelope carries invoker_id", async () => {
		await worker.fetch(
			new Request("https://example.com/v1/ingest", {
				method: "POST",
				body: JSON.stringify(envelopeWithInvoker),
			}),
		);

		await vi.waitFor(() => expect(fetchMock.mock.calls.length).toBeGreaterThanOrEqual(2));

		const bodies = fetchMock.mock.calls.map((call) => JSON.parse(call[1]?.body as string));
		const merge = bodies.find((b) => b.batch?.[0]?.event === "$merge_dangerously");
		expect(merge).toBeDefined();
		expect(merge.batch[0]).toMatchObject({
			event: "$merge_dangerously",
			distinct_id: envelopeWithInvoker.emitter_id,
			properties: { alias: envelopeWithInvoker.context.invoker_id },
		});
	});

	it("drops (200, no forward, no merge) when the rate limiter rejects", async () => {
		// Inject a limiter that denies. The full miniflare binding can't be
		// driven over its window deterministically, so exercise the branch by
		// overriding the binding on a direct handleIngest call. Use an envelope
		// that carries invoker_id so a single `fetchMock` assertion proves both
		// the PostHog forward and the $merge_dangerously fire are skipped.
		const limited = { ...env, INGEST_RATE_LIMITER: { limit: async () => ({ success: false }) } } as unknown as Env;
		const ctx = createExecutionContext();
		const res = await handleIngest(
			new Request("https://example.com/v1/ingest", { method: "POST", body: JSON.stringify(envelopeWithInvoker) }),
			limited,
			ctx,
		);
		await waitOnExecutionContext(ctx);

		expect(res.status).toBe(200);
		expect(fetchMock).not.toHaveBeenCalled();
	});

	it.each([
		["IPv4 keyed whole", "203.0.113.7", "203.0.113.7"],
		["IPv6 collapsed to the /64 prefix", "2001:db8:1:2:aaaa:bbbb:cccc:dddd", "2001:db8:1:2"],
		["IPv6 with a :: run expanded before truncating", "2001:db8::dead:beef", "2001:db8:0:0"],
		["missing header shares the 'unknown' bucket", null, "unknown"],
	])("rate-limit key: %s", async (_label, header, expectedKey) => {
		let seenKey: string | undefined;
		const spyEnv = {
			...env,
			INGEST_RATE_LIMITER: {
				limit: async ({ key }: { key: string }) => {
					seenKey = key;
					return { success: true };
				},
			},
		} as unknown as Env;
		const ctx = createExecutionContext();
		await handleIngest(
			new Request("https://example.com/v1/ingest", {
				method: "POST",
				headers: header === null ? {} : { "CF-Connecting-IP": header },
				body: JSON.stringify(validEnvelope),
			}),
			spyEnv,
			ctx,
		);
		await waitOnExecutionContext(ctx);

		expect(seenKey).toBe(expectedKey);
	});

	it("does not refire $merge_dangerously for a repeat (emitter, invoker) pair", async () => {
		await worker.fetch(
			new Request("https://example.com/v1/ingest", {
				method: "POST",
				body: JSON.stringify(envelopeWithInvoker),
			}),
		);
		await vi.waitFor(() => expect(fetchMock.mock.calls.length).toBeGreaterThanOrEqual(2));

		fetchMock.mockClear();

		await worker.fetch(
			new Request("https://example.com/v1/ingest", {
				method: "POST",
				body: JSON.stringify(envelopeWithInvoker),
			}),
		);
		await vi.waitFor(() => expect(fetchMock).toHaveBeenCalled());

		const bodies = fetchMock.mock.calls.map((call) => JSON.parse(call[1]?.body as string));
		const merges = bodies.filter((b) => b.batch?.[0]?.event === "$merge_dangerously");
		expect(merges).toHaveLength(0);
	});
});

describe("legacy notice routes", () => {
	it.each([
		["/", "/telemetry/"],
		["/cli", "/telemetry/cli/"],
		["/cli/", "/telemetry/cli/"],
		["/vscode", "/telemetry/vs-code/"],
	])("301-redirects %s to the docs hub", async (path, hubPath) => {
		// redirect: "manual" so the harness returns the 301 instead of
		// following it (a followed request would loop back into the worker
		// as an unhandled /telemetry/* path and 404). The hub origin comes
		// from the env var, so it's the staging docs URL under test.
		const res = await worker.fetch(new Request(`https://example.com${path}`, { redirect: "manual" }));
		expect(res.status).toBe(301);
		expect(res.headers.get("location")).toBe(`${env.DOCS_BASE_URL}${hubPath}`);
	});

	it("returns 405 for non-GET/HEAD on a notice route", async () => {
		const res = await worker.fetch(new Request("https://example.com/cli", { method: "POST" }));
		expect(res.status).toBe(405);
		expect(res.headers.get("allow")).toBe("GET, HEAD");
	});
});

describe("unmatched routes", () => {
	it("returns 404 for an unknown path", async () => {
		const res = await worker.fetch(new Request("https://example.com/unknown"));
		expect(res.status).toBe(404);
	});

	it("returns 405 for GET /v1/ingest (POST only)", async () => {
		const res = await worker.fetch(new Request("https://example.com/v1/ingest"));
		expect(res.status).toBe(405);
		expect(res.headers.get("allow")).toBe("POST");
	});
});
