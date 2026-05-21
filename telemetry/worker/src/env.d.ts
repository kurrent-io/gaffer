// Augments the wrangler-generated `Env` with secrets that aren't declared in
// `wrangler.jsonc` (and thus aren't picked up by `wrangler types`).
interface Env {
	// PostHog project API key. Set per env via
	// `wrangler secret put POSTHOG_API_KEY --env <staging|production>`.
	POSTHOG_API_KEY: string;
}

// Raw imports of .sql files for the test-time migration helper.
declare module "*.sql?raw" {
	const content: string;
	export default content;
}
