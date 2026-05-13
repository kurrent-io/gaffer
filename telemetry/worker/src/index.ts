import { prune } from "./cron";
import { handleIngest } from "./ingest";
import { handleNotice } from "./notice";

export default {
	async fetch(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
		const url = new URL(request.url);

		if (url.pathname === "/v1/ingest") {
			if (request.method !== "POST") {
				return new Response(null, { status: 405, headers: { allow: "POST" } });
			}
			return handleIngest(request, env, ctx);
		}

		const noticeResponse = handleNotice(url.pathname);
		if (noticeResponse !== null) {
			if (request.method !== "GET" && request.method !== "HEAD") {
				return new Response(null, { status: 405, headers: { allow: "GET, HEAD" } });
			}
			// HTTP HEAD must return the same headers/status as GET but with
			// no body. Workers doesn't auto-strip; do it explicitly.
			if (request.method === "HEAD") {
				return new Response(null, { status: noticeResponse.status, headers: noticeResponse.headers });
			}
			return noticeResponse;
		}

		if (url.pathname.startsWith("/fonts/") || url.pathname.startsWith("/favicons/")) {
			return env.ASSETS.fetch(request);
		}

		return new Response("Not Found", { status: 404 });
	},

	async scheduled(_event: ScheduledController, env: Env, _ctx: ExecutionContext): Promise<void> {
		// Await directly so cron failures surface as failed invocations in
		// the Cloudflare dashboard. Wrapping in ctx.waitUntil would resolve
		// the handler immediately and swallow errors into the cron's
		// success metrics.
		await prune(env.DB);
	},
} satisfies ExportedHandler<Env>;
