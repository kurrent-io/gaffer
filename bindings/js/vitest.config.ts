import { defineConfig } from "vitest/config";

export default defineConfig({
	test: {
		exclude: ["node_modules/**", "dist/**"],
		// Tests load a NativeAOT shared library via koffi; the cold start
		// can take several seconds on CI runners.
		testTimeout: 30_000,
	},
});
