// Vite supports loading any file as a raw string with `?raw`. The shim
// below is a focused declaration so we don't pull in the full
// `vite/client` triple-slash reference (which adds browser globals to
// the extension host's type environment).
declare module "*.html?raw" {
	const content: string;
	export default content;
}
