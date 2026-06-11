import { defineConfig } from "vitest/config";

export default defineConfig({
	test: {
		exclude: ["integration/**", "node_modules/**", "dist/**"],
		// Tests load a NativeAOT shared library via koffi; the cold start
		// can take several seconds on CI runners.
		testTimeout: 30_000,
		// Opt into the dev source-tree library fallback (see the binding's
		// findDevLibPath); set here, not inline in the npm script, so it works
		// on every platform.
		env: { GAFFER_RUNTIME_DEV: "1" },
	},
});
