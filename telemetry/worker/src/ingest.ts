import { Validator } from "@cfworker/json-schema";
import type { Envelope } from "@kurrent/gaffer-telemetry";
import schema from "../../generated/telemetry.schema.json" with { type: "json" };
import { maybeFireMerge } from "./merging";
import { stitchSession } from "./stitching";
import { translateEnvelope } from "./translation";

// Compiled once per isolate; reused across requests within the isolate's
// lifetime. @cfworker/json-schema is interpreter-based (no eval), so this is
// safe in the Cloudflare Workers runtime. Pin to JSON Schema draft 7 to match
// what the codegen pipeline emits.
const validator = new Validator(schema as never, "7");

export async function handleIngest(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
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

	// Stitch a session_id. Failure to stitch (D1 outage, schema drift) drops
	// the envelope entirely - sending events to PostHog without a $session_id
	// would land them in an undefined session and corrupt the cohort.
	let sessionId: string;
	try {
		sessionId = await stitchSession(envelope.emitter_id, envelope.run_id, env.DB);
	} catch {
		return ok();
	}

	// Fire identity merge if the envelope carries `invoker_id` (an extension-
	// spawned CLI invocation). Fire-and-forget; failure means the next event
	// for the same pair retries.
	const invokerId = envelope.context.invoker_id;
	if (typeof invokerId === "string") {
		ctx.waitUntil(
			maybeFireMerge(envelope.emitter_id, invokerId, env.DB, (eId, iId) =>
				fireMergeDangerously(env.POSTHOG_HOST, env.POSTHOG_API_KEY, eId, iId),
			),
		);
	}

	// Translate and forward fire-and-forget. PostHog ingest is best-effort;
	// the worker always returns 200 so the client doesn't retry against us.
	// Translation can throw on an unhandled variant; treat that as a drop too.
	let payload;
	try {
		payload = translateEnvelope(envelope, env.POSTHOG_API_KEY, sessionId);
	} catch {
		return ok();
	}
	ctx.waitUntil(forwardToPostHog(env.POSTHOG_HOST, payload));

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
