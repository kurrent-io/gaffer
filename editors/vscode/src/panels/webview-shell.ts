// Builds the HTML shell for a Solid webview. The real UI lives in a bundle
// under dist/webviews/ (built by vite.webviews.config.ts); this shell just
// mounts a root node, links the entry's stylesheet, and loads its module
// script - both resolved through `asWebviewUri` so they load from the
// extension's install location.
//
// CSP allows only our own resources (`cspSource`) and forbids inline script,
// so injected content can never execute as script; Solid escapes all
// interpolated text. Styles keep 'unsafe-inline' for the element style
// attributes Solid emits.

import * as vscode from "vscode";

// Entry basename as emitted by the webview build (deterministic, unhashed):
// dist/webviews/<entry>.js and dist/webviews/<entry>.css.
export type WebviewEntry = "status";

export function webviewHtml(
	webview: vscode.Webview,
	extensionUri: vscode.Uri,
	entry: WebviewEntry,
): string {
	const base = vscode.Uri.joinPath(extensionUri, "dist", "webviews");
	const script = webview.asWebviewUri(vscode.Uri.joinPath(base, `${entry}.js`));
	const style = webview.asWebviewUri(vscode.Uri.joinPath(base, `${entry}.css`));
	const csp = [
		"default-src 'none'",
		// base-uri and object-src don't inherit from default-src, so pin them.
		"base-uri 'none'",
		"object-src 'none'",
		`style-src ${webview.cspSource} 'unsafe-inline'`,
		`script-src ${webview.cspSource}`,
		`font-src ${webview.cspSource}`,
		`img-src ${webview.cspSource} data:`,
	].join("; ");
	return `<!doctype html>
<html lang="en">
	<head>
		<meta charset="UTF-8" />
		<meta http-equiv="Content-Security-Policy" content="${csp}" />
		<link rel="stylesheet" href="${style}" />
	</head>
	<body>
		<div id="root"></div>
		<script type="module" src="${script}"></script>
	</body>
</html>`;
}

// The localResourceRoots a webview must expose to load the bundle above.
export function webviewRoots(extensionUri: vscode.Uri): vscode.Uri[] {
	return [vscode.Uri.joinPath(extensionUri, "dist", "webviews")];
}
