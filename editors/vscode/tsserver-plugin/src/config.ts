// PluginConfig is the JSON payload exchanged between the extension
// and this plugin via the typescript-language-features API
// (configurePlugin). Defined here once and imported by both sides so
// the wire shape can't drift.
export interface PluginConfig {
	// Absolute path of the vendored projections.d.ts the extension
	// shipped. When unset (or when `enabled` is false) the plugin
	// no-ops.
	typesEntryPath?: string;
	enabled?: boolean;
}
