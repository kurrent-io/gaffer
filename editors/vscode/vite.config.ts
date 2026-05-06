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
		// Force Node resolution so vscode-languageclient/node doesn't
		// get rewritten to its browser variant via the legacy
		// `browser` field map. The browser variant uses Worker
		// transports and doesn't export TransportKind, so the spawn
		// blew up at runtime with "Cannot read properties of undefined
		// (reading 'stdio')".
		mainFields: ["module", "main"],
		conditions: ["node"],
	},
	test: {
		environment: "node",
		include: ["src/**/*.test.ts"],
		setupFiles: ["./test/setup.ts"],
		// Aliases scoped to the test runner only - production
		// builds must NOT pick up these stubs.
		alias: {
			"vscode-languageclient/node": path.resolve(
				here,
				"test/__mocks__/vscode-languageclient-node.ts",
			),
		},
	},
});
