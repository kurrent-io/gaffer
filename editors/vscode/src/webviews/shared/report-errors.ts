// Client-side error reporting for the webviews. Uncaught errors and unhandled
// rejections are posted to the host, which forwards them to telemetry under the
// "webview" phase (see panels-side telemetry/webview-error.ts). The webview
// can't reach the network itself (CSP default-src 'none'), so the host is the
// only egress. Render errors are reported separately by each root's
// ErrorBoundary fallback - Solid catches those, so they never reach onerror.

import type { WebviewErrorMessage } from "./webview-error-message.js";

export function errorToMessage(err: unknown): WebviewErrorMessage {
	const e = err instanceof Error ? err : new Error(String(err));
	return { command: "error", name: e.name, message: e.message, stack: e.stack };
}

export function installErrorReporting(
	report: (message: WebviewErrorMessage) => void,
): void {
	window.addEventListener("error", (event) => {
		// Resource-load failures fire "error" with neither .error nor a message;
		// there's no exception to report, so skip rather than send Error("undefined").
		if (!event.error && !event.message) return;
		report(errorToMessage(event.error ?? event.message));
	});
	window.addEventListener("unhandledrejection", (event) => {
		report(errorToMessage(event.reason));
	});
}
