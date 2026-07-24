// Message contract between the history viewer host (panels/history-view.ts) and
// the Solid webview. The host runs the classify->collapse->graph pipeline and
// posts the collapsed rows plus their lane layout; the webview only renders and
// posts back diff/rollback/cancel intents. Types are shared so the two sides
// can't drift.

import type { HistoryEntry } from "../../commands/history-schema.js";
import type { HistoryGraph } from "../../panels/history-graph.js";
import type { WebviewErrorMessage } from "../shared/webview-error-message.js";

export type { HistoryEntry, HistoryGraph };

// Host -> webview.
export type HistoryInbound =
	| {
			type: "history";
			name: string;
			env: string;
			entries: HistoryEntry[];
			graph: HistoryGraph;
			token: number;
	  }
	| { type: "error"; message: string }
	| { type: "rollback-active"; version: number }
	| { type: "rollback-done"; version: number; outcome: string }
	| { type: "rollback-error"; version: number; message: string };

// Webview -> host.
export type HistoryOutbound =
	| { command: "diff"; version: number; framing: "previous" | "local" }
	| { command: "rollback"; version: number; token: number }
	| WebviewErrorMessage;
