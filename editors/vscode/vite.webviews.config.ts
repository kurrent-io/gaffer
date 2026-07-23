import path from "node:path";
import { fileURLToPath } from "node:url";
import solid from "vite-plugin-solid";
import { defineConfig } from "vite";

// Webview bundles are a separate browser/ESM build from the extension's
// Node/CJS lib build, output to dist/webviews/ and loaded by the extension via
// `asWebviewUri` (panels/webview-shell.ts). Deterministic filenames let the
// shell reference `<entry>.js` / `<entry>.css` without a manifest.
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
		cssCodeSplit: true,
		rollupOptions: {
			input: {
				status: path.resolve(here, "src/webviews/status/main.tsx"),
			},
			output: {
				entryFileNames: "[name].js",
				chunkFileNames: "chunks/[name].js",
				assetFileNames: "[name][extname]",
			},
		},
	},
}));
