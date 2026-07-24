import { createSignal, ErrorBoundary } from "solid-js";
import { render } from "solid-js/web";
import "../shared/tokens.css";
import { errorToMessage, installErrorReporting } from "../shared/report-errors";
import { DeployPlan } from "./DeployPlan";
import type { DeployInbound, DeployOutbound } from "./protocol";

interface VsCodeApi {
	postMessage(message: DeployOutbound): void;
}
declare function acquireVsCodeApi(): VsCodeApi;

const vscode = acquireVsCodeApi();
// Queue inbound messages; <DeployPlan> drains them in order. A queue (not a
// single latest-value signal) keeps every streaming delta even if several
// arrive before the consumer runs (see the history webview for the same note).
const [inbox, setInbox] = createSignal<DeployInbound[]>([]);

installErrorReporting((m) => vscode.postMessage(m));

window.addEventListener("message", (event: MessageEvent) => {
	const msg = event.data as DeployInbound | undefined;
	if (msg && typeof msg.type === "string") setInbox((queue) => [...queue, msg]);
});

const root = document.getElementById("root");
if (root) {
	render(
		() => (
			<ErrorBoundary
				fallback={(err) => {
					vscode.postMessage(errorToMessage(err));
					return <div role="alert">Failed to render: {String(err)}</div>;
				}}
			>
				<DeployPlan
					inbox={inbox()}
					onDrained={() => setInbox([])}
					post={(m) => vscode.postMessage(m)}
				/>
			</ErrorBoundary>
		),
		root,
	);
}
