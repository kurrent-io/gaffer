import { defineConfig } from "vitest/config";

export default defineConfig({
	test: {
		include: ["integration/**/*.test.ts"],
		testTimeout: 30_000,
		// Opt into the dev source-tree library fallback (see the binding's
		// findDevLibPath); set here, not inline in the npm script, so it works
		// on every platform.
		env: { GAFFER_RUNTIME_DEV: "1" },
	},
});
