import { Validator } from "@cfworker/json-schema";
import type { Envelope } from "@kurrent/gaffer-telemetry";
import schema from "../../generated/telemetry.schema.json" with { type: "json" };
import { maybeFireMerge } from "./merging";
import { stitchSession } from "./stitching";
import { translateEnvelope } from "./translation";

// Compiled once per isolate; reused across requests within the isolate's
// lifetime. @cfworker/json-schema is interpreter-based (no eval), so this is
// safe in the Cloudflare Workers runtime. Pin to JSON Schema draft 2019-09
// so `unevaluatedProperties: false` works on variants that extend a base
// via `allOf` (draft 7's `additionalProperties: false` does not walk allOf
// and would reject base-inherited fields as "extras").
const validator = new Validator(schema as never, "2019-09");

// Cap the request body. A valid envelope with the schema's per-array
// `maxItems: 100` and string `maxLength` caps comes nowhere near a
// megabyte; anything larger is malformed or hostile. Reject before
// `request.json()` runs so we don't spend CPU parsing it.
const MAX_BODY_BYTES = 1024 * 1024;

// PostHog host allowlist. The notice page promises EU storage; the worker
// enforces it here so a misconfigured `wrangler.jsonc` (or a tampered
// staging override) can't silently send the API key to a different host.
const ALLOWED_POSTHOG_HOSTS = ["https://eu.i.posthog.com"];

export async function handleIngest(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
	// Reject oversized bodies before parsing.
	const contentLength = request.headers.get("content-length");
	if (contentLength !== null && Number.parseInt(contentLength, 10) > MAX_BODY_BYTES) {
		return ok();
	}

	// Refuse to send to anything other than the allowlisted PostHog host.
	// If config drift sneaks an alternate host in, we drop the envelope
	// rather than leak the API key.
	if (!ALLOWED_POSTHOG_HOSTS.includes(env.POSTHOG_HOST)) {
		console.error("rejecting envelope: POSTHOG_HOST not allowlisted:", env.POSTHOG_HOST);
		return ok();
	}

	// Read JSON. Drop on parse failure; never fail loudly on the request path.
	let envelope: Envelope;
	try {
		envelope = (await request.json()) as Envelope;
	} catch {
		return ok();
	}

	// Validate against the wire schema.
	const result = validator.validate(envelope);
	if (!result.valid) {
		return ok();
	}

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
	// waitUntil. Translation can throw on an unhandled variant; treat
	// that as a drop too.
	let payload;
	try {
		payload = translateEnvelope(envelope, env.POSTHOG_API_KEY, sessionId);
	} catch {
		return ok();
	}

	// Compose merge-then-forward into one waitUntil. Two separate
	// waitUntils would let isolate eviction land one POST but not the
	// other; chaining keeps the causal order (merge person link before
	// the stitched event arrives) and gives a single error site.
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
		await fetch(`${host}/batch`, {
			method: "POST",
			headers: { "content-type": "application/json" },
			body: JSON.stringify(payload),
		});
	} catch {
		// Network failure or PostHog outage. Drop. We could add a Cloudflare
		// Queue for replay later (out of scope per UI-1543).
	}
}

async function fireMergeDangerously(
	host: string,
	apiKey: string,
	emitterId: string,
	invokerId: string,
): Promise<boolean> {
	try {
		const res = await fetch(`${host}/batch`, {
			method: "POST",
			headers: { "content-type": "application/json" },
			body: JSON.stringify({
				api_key: apiKey,
				batch: [
					{
						event: "$merge_dangerously",
						distinct_id: emitterId,
						properties: { alias: invokerId },
					},
				],
			}),
		});
		return res.ok;
	} catch {
		return false;
	}
}

function ok(): Response {
	return new Response(null, { status: 200 });
}
