import { createSignal, ErrorBoundary } from "solid-js";
import { render } from "solid-js/web";
import "../shared/tokens.css";
import { errorToMessage, installErrorReporting } from "../shared/report-errors";
import { Status } from "./Status";
import type { StatusOutbound, StatusUpdateMessage } from "./protocol";

interface VsCodeApi {
	postMessage(message: StatusOutbound): void;
}
declare function acquireVsCodeApi(): VsCodeApi;

const vscode = acquireVsCodeApi();
const [update, setUpdate] = createSignal<StatusUpdateMessage | null>(null);

installErrorReporting((m) => vscode.postMessage(m));

window.addEventListener("message", (event: MessageEvent) => {
	const message = event.data as StatusUpdateMessage | undefined;
	if (message?.type === "update") setUpdate(message);
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
				<Status
					update={update()}
					onPause={() => vscode.postMessage({ command: "pause" })}
				/>
			</ErrorBoundary>
		),
		root,
	);
}
