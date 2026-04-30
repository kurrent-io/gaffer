/// <reference types="vitest" />
import { builtinModules } from "node:module";
import { defineConfig } from "vite";

export default defineConfig({
	build: {
		target: "node18",
		outDir: "dist",
		emptyOutDir: true,
		sourcemap: true,
		minify: false,
		lib: {
			entry: { extension: "src/extension.ts" },
			formats: ["cjs"],
			fileName: (_format, entryName) => `${entryName}.js`,
		},
		rollupOptions: {
			external: [
				"vscode",
				...builtinModules,
				...builtinModules.map((m) => `node:${m}`),
			],
			output: {
				exports: "named",
			},
		},
	},
	test: {
		environment: "node",
		include: ["src/**/*.test.ts"],
	},
});
