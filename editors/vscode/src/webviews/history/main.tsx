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
// Queue inbound messages; <History> drains them in order. A queue (not a
// single latest-value signal) means a delta can't be lost if several arrive
// before the consumer runs - we don't depend on the host posting one message
// per task.
const [inbox, setInbox] = createSignal<HistoryInbound[]>([]);

window.addEventListener("message", (event: MessageEvent) => {
	const msg = event.data as HistoryInbound | undefined;
	if (msg && typeof msg.type === "string") setInbox((queue) => [...queue, msg]);
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
				<History
					inbox={inbox()}
					onDrained={() => setInbox([])}
					post={(m) => vscode.postMessage(m)}
				/>
			</ErrorBoundary>
		),
		root,
	);
}
