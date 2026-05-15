import { cloudflareTest } from "@cloudflare/vitest-pool-workers";
import { defineConfig } from "vitest/config";

export default defineConfig({
	plugins: [
		cloudflareTest({
			// `main` is required so tests can `import { exports } from
			// "cloudflare:workers"` and call `exports.default.fetch()` against
			// the worker. The pool runs the entrypoint in the same isolate as
			// the tests, so this also makes module imports cohere.
			main: "src/index.ts",
			// Tests run against the staging env config so bindings (DB,
			// ASSETS, CF_VERSION_METADATA, POSTHOG_HOST) resolve. Staging
			// and production have the same binding shape; either would work.
			wrangler: { configPath: "./wrangler.jsonc", environment: "staging" },
			miniflare: {
				// Tests don't have access to the deploy-time secret, so set a
				// fixture value here. Real envs set POSTHOG_API_KEY via
				// `wrangler secret put POSTHOG_API_KEY --env <staging|production>`.
				bindings: {
					POSTHOG_API_KEY: "phc_test_fixture_key",
				},
			},
		}),
	],
});
