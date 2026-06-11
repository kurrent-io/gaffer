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
	// Per-source rate limit, checked before any body read so a flood is shed
	// at minimal cost. `/v1/ingest` is public and unauthenticated (telemetry
	// is anonymous - there's no token to check), so this is the only bound on
	// the D1 writes + PostHog forward each request triggers. The counter is
	// edge-local (per-colo), so it bounds a single-source flood; it does not
	// aggregate a distributed one - that's the accepted limit of the native
	// binding for a medium-severity telemetry endpoint. We drop (200) rather
	// than 429 to keep the "always 200" posture - the fire-and-forget client
	// ignores the status, and a distinct code would only hand an attacker a
	// signal to tune against. Fail open if the limiter itself errors: a blip
	// shouldn't drop all telemetry, and the guards below still apply.
	let allowed = true;
	try {
		allowed = (await env.INGEST_RATE_LIMITER.limit({ key: rateLimitKey(request) })).success;
	} catch (err) {
		console.error("rate limiter unavailable, allowing:", err);
	}
	if (!allowed) {
		console.error("drop: rate limit exceeded");
		return ok();
	}

	// Fast-path reject on an oversized Content-Length. The streaming check
	// below catches clients that omit or lie about the header.
	const contentLength = request.headers.get("content-length");
	if (contentLength !== null && Number.parseInt(contentLength, 10) > MAX_BODY_BYTES) {
		console.error("drop: content-length over cap:", contentLength);
		return ok();
	}

	if (!ALLOWED_POSTHOG_HOSTS.includes(env.POSTHOG_HOST)) {
		console.error("drop: POSTHOG_HOST not allowlisted:", env.POSTHOG_HOST);
		return ok();
	}

	const bodyText = await readBodyCapped(request, MAX_BODY_BYTES);
	if (bodyText === null) {
		console.error("drop: body unreadable or exceeded streaming cap");
		return ok();
	}

	let parsed: unknown;
	try {
		parsed = JSON.parse(bodyText);
	} catch (err) {
		// Log the parser error (includes position) and the body size, but
		// not the body content. /v1/ingest is public; logging raw payload
		// snippets would violate the worker's no-content-collection posture
		// when probes / misconfigured clients hit the endpoint.
		console.error("drop: JSON parse:", err, `(body=${bodyText.length}B)`);
		return ok();
	}

	const result = validator.validate(parsed);
	if (!result.valid) {
		// Log so client/schema drift surfaces in `wrangler tail` instead
		// of vanishing into "always 200, drop on failure". Truncated to
		// keep noisy bot-probe payloads from blowing the log budget.
		console.error("drop: schema invalid:", JSON.stringify(result.errors).slice(0, 500));
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
	} catch (err) {
		// Unhandled event variant or property-shape regression.
		console.error("drop: translation threw:", err);
		return ok();
	}

	// Chain merge then forward in one waitUntil so isolate eviction can't
	// land one POST without the other and the causal order (merge person
	// link before the stitched event) is preserved.
	//
	// The merge fires on the request-supplied (emitter_id, invoker_id) pair
	// without checking it against prior stitching state. Such a check can't
	// harden this: the endpoint is unauthenticated and the caller owns both
	// IDs, so any "invoker must have been seen" gate is satisfied by first
	// emitting an event for that invoker. It would block only fully-blind
	// random-pair merges while adding state to maintain. The realistic abuse
	// is volume (flooding junk merges to pollute PostHog / drive cost), which
	// the rate limit above bounds; a targeted merge of a specific victim
	// needs that victim's emitter_id, a random UUIDv4 that never leaves the
	// local state file except over TLS to this worker.
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
			// key, payload format change, project disabled). PostHog's error
			// bodies are small JSON; log the first 500 chars as-is.
			const body = await res.text();
			console.error("forwardToPostHog non-2xx:", res.status, body.slice(0, 500));
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

// Rate-limit key derived from CF-Connecting-IP, which Cloudflare sets from
// the TLS peer at the edge - clients can't forge it. IPv4 is one address per
// client, so it's keyed whole. For IPv6 a single client typically controls a
// whole /64, so we collapse to the /64 prefix (first four hextets) to stop an
// attacker rotating the low 64 bits for a fresh bucket per request. Requests
// that somehow lack the header share one "unknown" bucket rather than
// bypassing the limit.
function rateLimitKey(request: Request): string {
	const ip = request.headers.get("cf-connecting-ip");
	if (!ip) return "unknown";
	if (!ip.includes(":")) return ip;

	// Expand a `::` run so the /64 prefix is taken from the right groups.
	const [head, tail = ""] = ip.split("::");
	const headParts = head ? head.split(":") : [];
	const tailParts = tail ? tail.split(":") : [];
	const fill = Array(Math.max(0, 8 - headParts.length - tailParts.length)).fill("0");
	return [...headParts, ...fill, ...tailParts].slice(0, 4).join(":");
}

function ok(): Response {
	return new Response(null, { status: 200 });
}
