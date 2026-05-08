import { defineWorkersConfig } from "@cloudflare/vitest-pool-workers/config";

export default defineWorkersConfig({
	test: {
		poolOptions: {
			workers: {
				wrangler: { configPath: "./wrangler.jsonc" },
				miniflare: {
					// Tests don't have access to the deploy-time secret, so set a
					// fixture value here. Production sets POSTHOG_API_KEY via
					// `wrangler secret put`.
					bindings: {
						POSTHOG_API_KEY: "phc_test_fixture_key",
					},
				},
			},
		},
	},
});
