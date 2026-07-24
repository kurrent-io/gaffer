// Message contract between the deploy-plan host (panels/deploy-plan.ts) and the
// Solid webview. The host renders a read-only plan, then streams the apply back
// in place; the webview posts deploy/diff/cancel intents. Shared so the two
// sides can't drift.

import type { PlanItem, PlanReport } from "../../commands/deploy-plan.js";
import type { WebviewErrorMessage } from "../shared/webview-error-message.js";

export type { PlanItem, PlanReport };

// Outcome counts from the terminal deploy_summary line.
export interface DeploySummaryCounts {
	created: number;
	updated: number;
	rebuilt: number;
	skipped: number;
	refused: number;
	invalid: number;
	failed: number;
}

// Host -> webview.
export type DeployInbound =
	| { type: "plan"; report: PlanReport; token: number }
	| { type: "deploy-started" }
	| { type: "deploy-active"; name: string }
	| { type: "deploy-item"; name: string; outcome: string; detail?: string }
	| { type: "deploy-done"; summary: DeploySummaryCounts }
	| { type: "deploy-error"; message: string };

// Webview -> host.
export type DeployOutbound =
	| { command: "cancel" }
	| { command: "diff"; name: string }
	| { command: "deploy"; noValidate: boolean; token: number }
	| WebviewErrorMessage;
