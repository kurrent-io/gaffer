import * as fs from "node:fs";
import { builtinModules } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";
import type { Plugin } from "vite";
import { defineConfig } from "vitest/config";

const here = path.dirname(fileURLToPath(import.meta.url));

// The tsserver plugin (gaffer-tsserver-plugin) expects an absolute
// path to types/src/projections.d.ts at runtime so it can inject
// projection types into projection JS files. The extension's
// tsserver-plugin configurer reads this from disk under
// dist/types/, so the build step copies the source tree there.
function vendorProjectionTypes(): Plugin {
	const src = path.resolve(here, "../../types/src");
	const dest = path.resolve(here, "dist/types");
	return {
		name: "gaffer:vendor-projection-types",
		// `apply: 'build'` keeps this off the vitest path - vitest
		// otherwise picks the plugin up via its bundled vite, runs
		// writeBundle on test setup, and copies into dist/ on every
		// test invocation.
		apply: "build",
		buildStart() {
			if (!fs.existsSync(src)) {
				this.error(
					`vendor-projection-types: source ${src} missing - has types/src/ moved?`,
				);
			}
		},
		writeBundle() {
			fs.cpSync(src, dest, { recursive: true });
		},
	};
}

// Telemetry ingest URLs substituted at build time via `define`. Dev
// builds (`vite build`, `vite build --watch`, vitest) default to the
// staging worker so unreleased VSIX traffic doesn't pollute the prod
// PostHog project. The marketplace publish recipe runs with
// `--mode production` to flip to the prod URL.
//
// Both URLs are also hard-coded on the Go side: staging in
// cli/internal/telemetry/sink_http.go (`DefaultWorkerURL`), prod in
// cli/justfile (`build-release` ldflags). Keep them in lockstep if the
// worker URL ever moves.
const STAGING_INGEST_URL =
	"https://gaffer-telemetry-staging.kurrent.workers.dev/v1/ingest";
const PRODUCTION_INGEST_URL = "https://telemetry.gaffer.kurrent.io/v1/ingest";

export default defineConfig(({ mode }) => ({
	define: {
		__INGEST_URL__: JSON.stringify(
			mode === "production" ? PRODUCTION_INGEST_URL : STAGING_INGEST_URL,
		),
	},
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
	plugins: [vendorProjectionTypes()],
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
		include: ["src/**/*.test.ts", "tsserver-plugin/src/**/*.test.ts"],
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
}));
