// Route a webview's reported client-side error into the exception pipeline
// under the "webview" phase. buildException reads name/message/stack off an
// Error, so reconstruct a real one (a plain object isn't `instanceof Error` and
// would lose them); the message and stack frames are scrubbed downstream.

import type { WebviewErrorMessage } from "../webviews/shared/webview-error-message.js";
import type { Telemetry } from "./facade.js";

export function reportWebviewError(
	telemetry: Telemetry,
	msg: WebviewErrorMessage,
): void {
	const err = new Error(msg.message);
	err.name = msg.name;
	if (msg.stack !== undefined) err.stack = msg.stack;
	telemetry.reportException("webview", err);
}
