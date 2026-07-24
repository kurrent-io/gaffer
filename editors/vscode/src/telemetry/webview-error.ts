// Route a webview's reported client-side error into the exception pipeline
// under the "webview" phase. buildException reads name/message/stack off an
// Error, so reconstruct a real one (a plain object isn't `instanceof Error` and
// would lose them); the message and stack frames are scrubbed downstream.
//
// Inbound webview messages are untrusted at runtime - the panels only narrow on
// `command`, so validate the shape here rather than casting, and drop anything
// malformed so a future protocol drift can't emit garbage telemetry.

import type { WebviewErrorMessage } from "../webviews/shared/webview-error-message.js";
import type { Telemetry } from "./facade.js";

export function reportWebviewError(telemetry: Telemetry, msg: unknown): void {
	const parsed = parseWebviewError(msg);
	if (parsed === null) return;
	const err = new Error(parsed.message);
	err.name = parsed.name;
	if (parsed.stack !== undefined) err.stack = parsed.stack;
	telemetry.reportException("webview", err);
}

function parseWebviewError(msg: unknown): WebviewErrorMessage | null {
	if (typeof msg !== "object" || msg === null) return null;
	const m = msg as Record<string, unknown>;
	if (m.command !== "error") return null;
	if (typeof m.name !== "string" || typeof m.message !== "string") return null;
	if (m.stack !== undefined && typeof m.stack !== "string") return null;
	return { command: "error", name: m.name, message: m.message, stack: m.stack };
}
