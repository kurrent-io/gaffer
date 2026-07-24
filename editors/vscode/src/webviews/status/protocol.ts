// Message contract between the status WebviewView host (panels/status.ts) and
// the Solid webview (main.tsx). Shared by both so the compiler catches drift.
// The host owns all state and computes the rendered shape; the webview only
// renders what it receives and posts back the pause command.

import type { WebviewErrorMessage } from "../shared/webview-error-message.js";

export interface StatusUpdateMessage {
	type: "update";
	mode: "running" | "ended";
	title: string;
	stats: string[];
	showPauseButton: boolean;
	pauseButtonLabel: string;
	pauseButtonDisabled: boolean;
	// Reason the run failed; null when it hasn't. Must state the failure in
	// words - the ⚠ icon and red colour are decorative, so screen readers rely
	// on this text and the title.
	error: string | null;
}

// Host -> webview.
export type StatusInbound = StatusUpdateMessage;

// Webview -> host.
export interface StatusPauseCommand {
	command: "pause";
}
export type StatusOutbound = StatusPauseCommand | WebviewErrorMessage;
