import { Validator } from "@cfworker/json-schema";
import type { Envelope } from "@kurrent/gaffer-telemetry";
import schema from "../../generated/telemetry.schema.json" with { type: "json" };
import { fireMergeDangerously, maybeFireMerge } from "./merging";
import { stitchSession } from "./stitching";
import { translateEnvelope } from "./translation";

// Compiled once per isolate; reused across requests within the isolate's
// lifetime. @cfworker/json-schema is interpreter-based (no eval), so this is
// safe in the Cloudflare Workers runtime. Pin to JSON Schema draft 2019-09
// so `unevaluatedProperties: false` works on variants that extend a base
// via `allOf` (draft 7's `additionalProperties: false` does not walk allOf
// and would reject base-inherited fields as "extras").
const validator = new Validator(schema as never, "2019-09");

// A valid envelope with the schema's per-array `maxItems: 100` and string
// `maxLength` caps comes nowhere near a megabyte; anything larger is
// malformed or hostile.
const MAX_BODY_BYTES = 1024 * 1024;

// PostHog host allowlist. The notice page promises EU storage; the worker
// enforces it here so a misconfigured `wrangler.jsonc` (or a tampered
// staging override) can't silently send the API key to a different host.
const ALLOWED_POSTHOG_HOSTS = ["https://eu.i.posthog.com"];

export async function handleIngest(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
	// Fast-path reject on an oversized Content-Length. The streaming check
	// below catches clients that omit or lie about the header.
	const contentLength = request.headers.get("content-length");
	if (contentLength !== null && Number.parseInt(contentLength, 10) > MAX_BODY_BYTES) {
		return ok();
	}

	if (!ALLOWED_POSTHOG_HOSTS.includes(env.POSTHOG_HOST)) {
		console.error("rejecting envelope: POSTHOG_HOST not allowlisted:", env.POSTHOG_HOST);
		return ok();
	}

	const bodyText = await readBodyCapped(request, MAX_BODY_BYTES);
	if (bodyText === null) {
		return ok();
	}

	let parsed: unknown;
	try {
		parsed = JSON.parse(bodyText);
	} catch {
		return ok();
	}

	const result = validator.validate(parsed);
	if (!result.valid) {
		return ok();
	}
	const envelope = parsed as Envelope;

	// Stitch a session_id. If stitching fails (D1 outage, schema drift)
	// drop the envelope rather than forwarding it without `$session_id`.
	// PostHog tolerates events that lack the property - they just don't
	// appear in session funnels - but the worker's job is to produce
	// session-stitched data, and a silent gap in coverage is worse than
	// a deliberate drop we can see in counters.
	let sessionId: string;
	try {
		sessionId = await stitchSession(envelope.emitter_id, envelope.run_id, env.DB);
	} catch (err) {
		console.error("stitchSession failed:", err);
		return ok();
	}

	// Translate up-front so the merge + forward can share a single
	// waitUntil. Translation can throw on an unhandled variant; drop.
	let payload;
	try {
		const batch = translateEnvelope(envelope, sessionId, env.CF_VERSION_METADATA.timestamp);
		payload = { api_key: env.POSTHOG_API_KEY, batch };
	} catch {
		return ok();
	}

	// Chain merge then forward in one waitUntil so isolate eviction can't
	// land one POST without the other and the causal order (merge person
	// link before the stitched event) is preserved.
	const invokerId = envelope.context.invoker_id;
	ctx.waitUntil(
		(async () => {
			if (typeof invokerId === "string") {
				try {
					await maybeFireMerge(envelope.emitter_id, invokerId, env.DB, (eId, iId) =>
						fireMergeDangerously(env.POSTHOG_HOST, env.POSTHOG_API_KEY, eId, iId),
					);
				} catch (err) {
					console.error("maybeFireMerge failed:", err);
				}
			}
			await forwardToPostHog(env.POSTHOG_HOST, payload);
		})(),
	);

	return ok();
}

async function forwardToPostHog(host: string, payload: unknown): Promise<void> {
	try {
		const res = await fetch(`${host}/batch`, {
			method: "POST",
			headers: { "content-type": "application/json" },
			body: JSON.stringify(payload),
			// Cap the outbound request so a hung PostHog doesn't sit on the
			// isolate's waitUntil budget.
			signal: AbortSignal.timeout(5000),
		});
		if (!res.ok) {
			// With "always 200, drop on failure" up the stack, console.error
			// is the only signal that envelopes are being rejected (bad API
			// key, payload format change, project disabled).
			console.error("forwardToPostHog non-2xx:", res.status);
		}
	} catch (err) {
		console.error("forwardToPostHog fetch failed:", err);
	}
}

// Read the body as a single string, aborting if total bytes exceed `max`.
// Returns null on overflow or no body. We accumulate chunks and decode at
// the end so a multi-byte UTF-8 character can't be split across two chunks.
async function readBodyCapped(request: Request, max: number): Promise<string | null> {
	const reader = request.body?.getReader();
	if (!reader) return null;
	const chunks: Uint8Array[] = [];
	let total = 0;
	try {
		while (true) {
			const { done, value } = await reader.read();
			if (done) break;
			total += value.byteLength;
			if (total > max) {
				await reader.cancel();
				return null;
			}
			chunks.push(value);
		}
	} catch {
		return null;
	}
	const buf = new Uint8Array(total);
	let offset = 0;
	for (const c of chunks) {
		buf.set(c, offset);
		offset += c.byteLength;
	}
	return new TextDecoder().decode(buf);
}

function ok(): Response {
	return new Response(null, { status: 200 });
}
