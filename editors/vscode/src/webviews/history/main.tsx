import { createSignal, ErrorBoundary } from "solid-js";
import { render } from "solid-js/web";
import "../shared/tokens.css";
import { History } from "./History";
import type { HistoryInbound, HistoryOutbound } from "./protocol";

interface VsCodeApi {
	postMessage(message: HistoryOutbound): void;
}
declare function acquireVsCodeApi(): VsCodeApi;

const vscode = acquireVsCodeApi();
// One signal as the inbound inbox. Safe because each host post arrives as its
// own MessageEvent task, so the effect runs once per message; don't refactor
// the host to post two messages in a single synchronous turn or one would drop.
const [message, setMessage] = createSignal<HistoryInbound | undefined>(
	undefined,
);

window.addEventListener("message", (event: MessageEvent) => {
	const msg = event.data as HistoryInbound | undefined;
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
				<History message={message()} post={(m) => vscode.postMessage(m)} />
			</ErrorBoundary>
		),
		root,
	);
}
