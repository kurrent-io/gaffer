import { prune } from "./cron";
import { handleIngest } from "./ingest";
import { handleNotice } from "./notice";

export default {
	async fetch(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
		const url = new URL(request.url);

		if (url.pathname === "/v1/ingest" && request.method === "POST") {
			return handleIngest(request, env, ctx);
		}

		if (url.pathname === "/" && request.method === "GET") {
			return handleNotice();
		}

		if (url.pathname.startsWith("/fonts/") || url.pathname.startsWith("/favicons/")) {
			return env.ASSETS.fetch(request);
		}

		return new Response("Not Found", { status: 404 });
	},

	async scheduled(_event: ScheduledController, env: Env, ctx: ExecutionContext): Promise<void> {
		ctx.waitUntil(prune(env.DB));
	},
} satisfies ExportedHandler<Env>;
