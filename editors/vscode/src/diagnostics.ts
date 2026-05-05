// VS Code DiagnosticCollection for fatal-error squiggles in the
// editor + entries in the Problems panel. Module-level singleton -
// like output.ts and notifications.ts, this is write-only ambient
// infrastructure that doesn't fit the snapshot-factory pattern.
//
// Two collections live here:
// - "gaffer" tracks runtime fatal errors from a debug session; cleared
//   at session start.
// - "gaffer-toml" tracks static gaffer.toml validation issues (invalid
//   fixtures); driven by lens re-evaluation and never cleared by the
//   session lifecycle.

import * as vscode from "vscode";

let collection: vscode.DiagnosticCollection | null = null;
let tomlCollection: vscode.DiagnosticCollection | null = null;

export function initDiagnostics(context: vscode.ExtensionContext): void {
	collection = vscode.languages.createDiagnosticCollection("gaffer");
	tomlCollection = vscode.languages.createDiagnosticCollection("gaffer-toml");
	context.subscriptions.push(collection, tomlCollection);
}

export interface FatalErrorReport {
	file: string;
	line: number | undefined;
	column: number | undefined;
	code: string;
	description: string;
	jsStack: string | undefined;
	eventId: string | undefined;
}

export function reportFatalError(report: FatalErrorReport): void {
	if (!collection) return;
	const uri = vscode.Uri.file(report.file);
	// CLI emits 1-based positions; VS Code uses 0-based.
	const line = Math.max(0, (report.line ?? 1) - 1);
	const column = Math.max(0, (report.column ?? 1) - 1);
	const range = new vscode.Range(line, column, line, column + 1);

	// First line is the headline (single-line truncate in Problems
	// panel); jsStack on a separated block shows in the hover view.
	// TODO: parse stack frames into Diagnostic.relatedInformation for
	// clickable navigation.
	const headline = report.eventId
		? `${report.code} (event ${report.eventId}): ${report.description}`
		: `${report.code}: ${report.description}`;
	const message = report.jsStack
		? `${headline}\n\n${report.jsStack}`
		: headline;

	const diag = new vscode.Diagnostic(
		range,
		message,
		vscode.DiagnosticSeverity.Error,
	);
	diag.source = "gaffer";
	collection.set(uri, [diag]);
}

export function clearDiagnostics(): void {
	collection?.clear();
}

// Clear runtime fatal-error diagnostics for a single URI. Used when
// the user starts editing the file (stale-on-edit) or explicitly
// dismisses via the code action. Cheap when the URI has no entries.
export function clearDiagnosticsForUri(uri: vscode.Uri): void {
	collection?.delete(uri);
}

// Code action provider for runtime fatal-error diagnostics: offers
// "Dismiss this error" for any source: "gaffer" entry. Lets the user
// clear the squiggle without editing the file (e.g. they want to
// retry the projection after fixing something else).
export class DismissDiagnosticActionProvider
	implements vscode.CodeActionProvider
{
	provideCodeActions(
		document: vscode.TextDocument,
		_range: vscode.Range | vscode.Selection,
		context: vscode.CodeActionContext,
	): vscode.CodeAction[] {
		return context.diagnostics
			.filter((d) => d.source === "gaffer")
			.map((d) => {
				const action = new vscode.CodeAction(
					"Dismiss this error",
					vscode.CodeActionKind.QuickFix,
				);
				action.command = {
					title: "Dismiss",
					command: "gaffer.dismissDiagnostic",
					arguments: [document.uri],
				};
				action.diagnostics = [d];
				return action;
			});
	}
}

// Replace this URI's toml-validation diagnostics with the given list.
// Pass an empty array to clear them. Empty array also leaves the
// "gaffer-toml" Problems-panel section absent rather than showing a
// stale "no problems" entry.
export function setTomlDiagnostics(
	uri: vscode.Uri,
	diagnostics: vscode.Diagnostic[],
): void {
	if (!tomlCollection) return;
	if (diagnostics.length === 0) tomlCollection.delete(uri);
	else tomlCollection.set(uri, diagnostics);
}

// Test-only: drop the cached collections so the next initDiagnostics
// in a fresh test starts clean. Production never calls this.
export function __resetForTest(): void {
	collection = null;
	tomlCollection = null;
}
