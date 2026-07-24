// The webview -> host error report shape. Dependency-free (no DOM, no host
// imports) so both the webview (report-errors.ts) and the extension host
// (telemetry/webview-error.ts, the panels) can import the type without dragging
// each other's environment in.
export interface WebviewErrorMessage {
	command: "error";
	name: string;
	message: string;
	stack?: string | undefined;
}
