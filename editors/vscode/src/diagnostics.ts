// VS Code DiagnosticCollection for fatal-error squiggles in the
// editor + entries in the Problems panel. Module-level singleton -
// like output.ts and notifications.ts, this is write-only ambient
// infrastructure that doesn't fit the snapshot-factory pattern.

import * as vscode from "vscode";

let collection: vscode.DiagnosticCollection | null = null;

export function initDiagnostics(context: vscode.ExtensionContext): void {
	collection = vscode.languages.createDiagnosticCollection("gaffer");
	context.subscriptions.push(collection);
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
