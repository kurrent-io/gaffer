// Augments the wrangler-generated `Cloudflare.Env` with secrets that aren't
// declared in `wrangler.jsonc` (and thus aren't picked up by `wrangler types`).
declare namespace Cloudflare {
	interface Env {
		// PostHog project API key. Set via `wrangler secret put POSTHOG_API_KEY`.
		POSTHOG_API_KEY: string;
	}
}

// Raw imports of .sql files for the test-time migration helper.
declare module "*.sql?raw" {
	const content: string;
	export default content;
}
