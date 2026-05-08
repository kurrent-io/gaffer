import { Validator } from "@cfworker/json-schema";
import type { Envelope } from "@kurrent/gaffer-telemetry";
import schema from "../../generated/telemetry.schema.json" with { type: "json" };
import type { Env } from "./index";
import { translateEnvelope } from "./translation";

// Compiled once per isolate; reused across requests within the isolate's
// lifetime. @cfworker/json-schema is interpreter-based (no eval), so this is
// safe in the Cloudflare Workers runtime.
const validator = new Validator(schema as never);

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

	// Translate and forward fire-and-forget. PostHog ingest is best-effort;
	// the worker always returns 200 so the client doesn't retry against us.
	const payload = translateEnvelope(envelope, env.POSTHOG_API_KEY);
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

function ok(): Response {
	return new Response(null, { status: 200 });
}
