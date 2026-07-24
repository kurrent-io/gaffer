import path from "node:path";
import { fileURLToPath } from "node:url";
import solid from "vite-plugin-solid";
import { defineConfig } from "vite";

// Webview bundles are a separate browser/ESM build from the extension's
// Node/CJS lib build, output to dist/webviews/ and loaded by the extension via
// `asWebviewUri` (panels/webview-shell.ts). Deterministic filenames let the
// shell reference `<entry>.js` (all entries share one style.css) without a
// manifest.
const here = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig(({ mode }) => ({
	root: here,
	plugins: [solid()],
	build: {
		target: "esnext",
		outDir: "dist/webviews",
		// Off: the extension build's dist/ clean would wipe this sibling in
		// watch mode. Full builds clean dist/ first (see package.json scripts).
		emptyOutDir: false,
		sourcemap: mode !== "production",
		// One combined stylesheet (style.css) rather than per-entry/per-chunk
		// CSS: the shell links a single deterministic file, and a shared chunk's
		// CSS can't go unlinked. JS still splits freely (shared chunks load via
		// the module graph, allowed by the cspSource script-src).
		cssCodeSplit: false,
		rollupOptions: {
			input: {
				status: path.resolve(here, "src/webviews/status/main.tsx"),
				history: path.resolve(here, "src/webviews/history/main.tsx"),
			},
			output: {
				entryFileNames: "[name].js",
				chunkFileNames: "chunks/[name].js",
				assetFileNames: "[name][extname]",
			},
		},
	},
}));
