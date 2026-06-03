import { prune } from "./cron";
import { handleIngest } from "./ingest";
import { handleNoticeRedirect } from "./redirects";

export default {
	async fetch(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
		const url = new URL(request.url);

		if (url.pathname === "/v1/ingest") {
			if (request.method !== "POST") {
				return new Response(null, { status: 405, headers: { allow: "POST" } });
			}
			return handleIngest(request, env, ctx);
		}

		const redirect = handleNoticeRedirect(url.pathname, env.DOCS_BASE_URL);
		if (redirect !== null) {
			if (request.method !== "GET" && request.method !== "HEAD") {
				return new Response(null, { status: 405, headers: { allow: "GET, HEAD" } });
			}
			// A 301 carries no body, so the same response is valid for HEAD.
			return redirect;
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
