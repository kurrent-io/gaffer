// Map VS Code-family editor identification to the schema's Editor
// enum. `vscode.env.appName` is the canonical signal; the prefix
// matching covers the common forks (VSCodium, Cursor, Windsurf).
// Anything else falls through to "other" - the worker still records
// the envelope, just without a known editor breakdown.

import type { Editor } from "@kurrent/gaffer-telemetry";

/**
 * Map `vscode.env.appName` to the schema's Editor enum. The matcher
 * is case-insensitive substring on purpose: "Cursor (Insiders)" and
 * "Code - OSS" both want their canonical id, not "other".
 */
export function detectEditor(appName: string): Editor {
	const n = appName.toLowerCase();
	if (n.includes("cursor")) return "cursor";
	if (n.includes("windsurf")) return "windsurf";
	if (n.includes("vscodium") || n.includes("code - oss")) return "vscodium";
	if (n.includes("visual studio code") || n.includes("code")) return "vscode";
	return "other";
}
