import { createSignal, ErrorBoundary } from "solid-js";
import { render } from "solid-js/web";
import "../shared/tokens.css";
import { DeployPlan } from "./DeployPlan";
import type { DeployInbound, DeployOutbound } from "./protocol";

interface VsCodeApi {
	postMessage(message: DeployOutbound): void;
}
declare function acquireVsCodeApi(): VsCodeApi;

const vscode = acquireVsCodeApi();
// One signal as the inbound inbox; safe because each host post arrives as its
// own MessageEvent task (see the history webview for the same note).
const [message, setMessage] = createSignal<DeployInbound | undefined>(
	undefined,
);

window.addEventListener("message", (event: MessageEvent) => {
	const msg = event.data as DeployInbound | undefined;
	if (msg && typeof msg.type === "string") setMessage(msg);
});

const root = document.getElementById("root");
if (root) {
	render(
		() => (
			<ErrorBoundary
				fallback={(err) => (
					<div role="alert">Failed to render: {String(err)}</div>
				)}
			>
				<DeployPlan message={message()} post={(m) => vscode.postMessage(m)} />
			</ErrorBoundary>
		),
		root,
	);
}
