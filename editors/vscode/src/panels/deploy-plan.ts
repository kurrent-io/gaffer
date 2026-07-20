// Editor-area webview that renders a deploy plan (a `gaffer deploy --dry-run`
// result): per-projection change kind and warnings, the env/target header and
// verdict, and any [database_config] drift. Read-only - clicking a projection
// opens its native source diff.
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

interface PlanMessage {
	type: "plan";
	report: PlanReport;
}

export class DeployPlanView implements vscode.Disposable {
	#panel: vscode.WebviewPanel | undefined;
	#ctx: DeployPlanContext | undefined;
	// Invoked when the webview asks to diff a projection (a row click). Fixed at
	// construction so tests can assert the dispatch without a live diff command.
	readonly #onDiff: (ctx: DeployPlanContext, name: string) => void;

	constructor(onDiff: (ctx: DeployPlanContext, name: string) => void) {
		this.#onDiff = onDiff;
	}

	// Show the plan for (env, project): create the panel on first use, otherwise
	// reveal and re-render the existing one. The env/target context rides along so
	// a row-click diff resolves against the right env and gaffer.toml.
	show(report: PlanReport, ctx: DeployPlanContext): void {
		this.#ctx = ctx;
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
			});
		}
		this.#panel.title = planTitle(ctx.env);
		this.#panel.reveal(this.#panel.viewColumn);
		const message: PlanMessage = { type: "plan", report };
		void this.#panel.webview.postMessage(message);
	}

	#handleMessage(msg: unknown): void {
		if (
			typeof msg === "object" &&
			msg !== null &&
			(msg as { command?: unknown }).command === "diff" &&
			typeof (msg as { name?: unknown }).name === "string" &&
			this.#ctx
		) {
			this.#onDiff(this.#ctx, (msg as { name: string }).name);
		}
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
