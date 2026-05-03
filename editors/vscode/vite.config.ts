import { builtinModules } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

const here = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig({
	build: {
		target: "node18",
		outDir: "dist",
		emptyOutDir: true,
		sourcemap: false,
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
	resolve: {
		alias: {
			vscode: path.resolve(here, "test/__mocks__/vscode.ts"),
		},
	},
	test: {
		environment: "node",
		include: ["src/**/*.test.ts"],
		setupFiles: ["./test/setup.ts"],
	},
});
