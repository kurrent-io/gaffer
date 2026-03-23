import { defineConfig } from "vitest/config";

export default defineConfig({
	test: {
		exclude: ["integration/**", "node_modules/**", "dist/**"],
	},
});
