// Editor-area webview that renders a deploy plan (a `gaffer deploy --dry-run`
// result): the env/target header, each projection's change kind and warnings, a
// per-action summary, and any [database_config] drift. An updated projection's
// Diff button opens its native source diff. The plan opens read-only; the
// webview's Deploy button applies it and the apply streams back in place.
//
// One reusable panel: a new preview reveals and re-renders the existing tab
// rather than stacking tabs. HTML lives in deploy-plan.html (loaded raw at build
// time); rendered once, then updated via postMessage so a re-preview doesn't
// drop scroll/focus. CSP is locked to the loaded nonce and the webview's
// cspSource for styles; localResourceRoots is empty since the template is
// self-contained.

import * as vscode from "vscode";
import type { PlanReport } from "../commands/deploy-plan.js";
import deployPlanTemplate from "./deploy-plan.html?raw";

export interface DeployPlanContext {
	env: string;
	tomlUri: vscode.Uri;
}

// The outcome counts from the terminal deploy_summary NDJSON line.
export interface DeploySummaryCounts {
	created: number;
	updated: number;
	rebuilt: number;
	skipped: number;
	refused: number;
	invalid: number;
	failed: number;
}

// Extension -> webview. `plan` renders the (read-only) plan; the rest drive the
// apply: `deploy-started` switches the view into progress mode, `deploy-item`
// settles one projection's row, `deploy-done` shows the final result summary, and
// `deploy-error` reports an apply that couldn't run.
export type DeployPlanMessage =
	| { type: "plan"; report: PlanReport }
	| { type: "deploy-started" }
	| { type: "deploy-active"; name: string }
	| { type: "deploy-item"; name: string; outcome: string; detail?: string }
	| { type: "deploy-done"; summary: DeploySummaryCounts }
	| { type: "deploy-error"; message: string };

// Sends progress from an in-flight apply back to the webview.
export type DeploySend = (message: DeployPlanMessage) => void;

export interface DeployPlanHandlers {
	// A projection's Diff button (update rows only).
	onDiff: (ctx: DeployPlanContext, name: string) => void;
	// The webview's Deploy button. `noValidate` is the "deploy the valid ones,
	// skip the rest" bypass. `send` streams progress back to this panel.
	onDeploy: (
		ctx: DeployPlanContext,
		report: PlanReport,
		noValidate: boolean,
		send: DeploySend,
	) => void;
}

export class DeployPlanView implements vscode.Disposable {
	#panel: vscode.WebviewPanel | undefined;
	#ctx: DeployPlanContext | undefined;
	#report: PlanReport | undefined;
	readonly #handlers: DeployPlanHandlers;

	constructor(handlers: DeployPlanHandlers) {
		this.#handlers = handlers;
	}

	// Show the plan for (env, project): create the panel on first use, otherwise
	// reveal and re-render the existing one. The env/target context and the report
	// ride along so a Diff or Deploy resolves against the right env and plan.
	show(report: PlanReport, ctx: DeployPlanContext): void {
		this.#ctx = ctx;
		this.#report = report;
		if (!this.#panel) {
			this.#panel = vscode.window.createWebviewPanel(
				"gaffer.deployPlan",
				planTitle(ctx.env),
				{ viewColumn: vscode.ViewColumn.Active, preserveFocus: false },
				{
					enableScripts: true,
					retainContextWhenHidden: true,
					localResourceRoots: [],
				},
			);
			const nonce = generateNonce();
			this.#panel.webview.html = deployPlanTemplate
				.replaceAll("{{NONCE}}", nonce)
				.replaceAll("{{CSP_SOURCE}}", this.#panel.webview.cspSource);
			this.#panel.webview.onDidReceiveMessage((msg: unknown) => {
				this.#handleMessage(msg);
			});
			this.#panel.onDidDispose(() => {
				this.#panel = undefined;
				this.#ctx = undefined;
				this.#report = undefined;
			});
		}
		this.#panel.title = planTitle(ctx.env);
		this.#panel.reveal(this.#panel.viewColumn);
		this.#post({ type: "plan", report });
	}

	#handleMessage(msg: unknown): void {
		if (typeof msg !== "object" || msg === null || !this.#ctx) return;
		const command = (msg as { command?: unknown }).command;
		if (command === "cancel") {
			this.dispose();
			return;
		}
		if (
			command === "diff" &&
			typeof (msg as { name?: unknown }).name === "string"
		) {
			this.#handlers.onDiff(this.#ctx, (msg as { name: string }).name);
			return;
		}
		if (command === "deploy" && this.#report) {
			const noValidate = (msg as { noValidate?: unknown }).noValidate === true;
			this.#handlers.onDeploy(this.#ctx, this.#report, noValidate, (m) =>
				this.#post(m),
			);
		}
	}

	#post(message: DeployPlanMessage): void {
		void this.#panel?.webview.postMessage(message);
	}

	dispose(): void {
		this.#panel?.dispose();
		this.#panel = undefined;
	}
}

function planTitle(env: string): string {
	return `Deploy plan: ${env}`;
}

function generateNonce(): string {
	return crypto.randomUUID().replaceAll("-", "");
}
