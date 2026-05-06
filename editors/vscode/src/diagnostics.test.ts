import * as vscode from "vscode";
import { describe, expect, it } from "vitest";
import {
	DismissDiagnosticActionProvider,
	clearDiagnostics,
	clearDiagnosticsForUri,
	initDiagnostics,
	reportFatalError,
} from "./diagnostics.js";
import { makeContext } from "../test/testutil/fake-context.js";
import { getState } from "../test/testutil/vscode-state.js";

describe("diagnostics", () => {
	it("reportFatalError is a no-op when not initialised", () => {
		// __resetForTest in setup nulls the cached collection.
		reportFatalError({
			file: "/p/projection.js",
			line: 1,
			column: 1,
			code: "JS_ERROR",
			description: "x",
			jsStack: undefined,
			eventId: undefined,
		});
		expect(getState().diagnosticCollections).toEqual([]);
	});

	it("attaches a single diagnostic at the reported position (1-based -> 0-based)", () => {
		initDiagnostics(makeContext());
		reportFatalError({
			file: "/p/projection.js",
			line: 5,
			column: 3,
			code: "JS_ERROR",
			description: "boom",
			jsStack: undefined,
			eventId: undefined,
		});
		const collection = getState().diagnosticCollections[0];
		const entries = collection?.entries.get("/p/projection.js");
		expect(entries).toHaveLength(1);
		const diag = entries?.[0];
		expect(diag?.range.start.line).toBe(4);
		expect(diag?.range.start.character).toBe(2);
		expect(diag?.range.end.line).toBe(4);
		expect(diag?.range.end.character).toBe(3);
		expect(diag?.source).toBe("gaffer");
	});

	it("clamps undefined or zero line/column to 0", () => {
		initDiagnostics(makeContext());
		reportFatalError({
			file: "/p/projection.js",
			line: undefined,
			column: undefined,
			code: "JS_ERROR",
			description: "boom",
			jsStack: undefined,
			eventId: undefined,
		});
		const diag = getState()
			.diagnosticCollections[0]?.entries.get("/p/projection.js")
			?.at(0);
		expect(diag?.range.start.line).toBe(0);
		expect(diag?.range.start.character).toBe(0);
	});

	it("includes eventId in the headline when present", () => {
		initDiagnostics(makeContext());
		reportFatalError({
			file: "/p/projection.js",
			line: 1,
			column: 1,
			code: "JS_ERROR",
			description: "boom",
			jsStack: undefined,
			eventId: "evt-7",
		});
		const diag = getState()
			.diagnosticCollections[0]?.entries.get("/p/projection.js")
			?.at(0);
		expect(diag?.message).toBe("JS_ERROR (event evt-7): boom");
	});

	it("appends jsStack on a separated block", () => {
		initDiagnostics(makeContext());
		reportFatalError({
			file: "/p/projection.js",
			line: 1,
			column: 1,
			code: "JS_ERROR",
			description: "boom",
			jsStack: "at handler\nat caller",
			eventId: undefined,
		});
		const diag = getState()
			.diagnosticCollections[0]?.entries.get("/p/projection.js")
			?.at(0);
		expect(diag?.message).toBe("JS_ERROR: boom\n\nat handler\nat caller");
	});

	it("clearDiagnosticsForUri removes only the given URI's entries", () => {
		initDiagnostics(makeContext());
		reportFatalError({
			file: "/p/a.js",
			line: 1,
			column: 1,
			code: "JS_ERROR",
			description: "x",
			jsStack: undefined,
			eventId: undefined,
		});
		reportFatalError({
			file: "/p/b.js",
			line: 1,
			column: 1,
			code: "JS_ERROR",
			description: "y",
			jsStack: undefined,
			eventId: undefined,
		});
		const coll = getState().diagnosticCollections[0];
		// reportFatalError uses .set which replaces; both URIs end up
		// with one entry each only because they're separate URIs.
		expect(coll?.entries.has("/p/a.js")).toBe(true);
		expect(coll?.entries.has("/p/b.js")).toBe(true);

		clearDiagnosticsForUri(vscode.Uri.file("/p/a.js"));
		expect(coll?.entries.has("/p/a.js")).toBe(false);
		expect(coll?.entries.has("/p/b.js")).toBe(true);
	});

	it("clearDiagnosticsForUri is a no-op when not initialised", () => {
		clearDiagnosticsForUri(vscode.Uri.file("/p/a.js"));
		expect(getState().diagnosticCollections).toEqual([]);
	});

	it("DismissDiagnosticActionProvider returns a Dismiss action for gaffer diagnostics", () => {
		const provider = new DismissDiagnosticActionProvider();
		const uri = vscode.Uri.file("/p/projection.js");
		const doc = { uri } as vscode.TextDocument;
		const range = new vscode.Range(0, 0, 0, 1);
		const gafferDiag = new vscode.Diagnostic(
			range,
			"boom",
			vscode.DiagnosticSeverity.Error,
		);
		gafferDiag.source = "gaffer";
		const otherDiag = new vscode.Diagnostic(
			range,
			"unrelated",
			vscode.DiagnosticSeverity.Error,
		);
		otherDiag.source = "tsc";
		const ctx = {
			diagnostics: [gafferDiag, otherDiag],
			only: undefined,
			triggerKind: 1,
		} as unknown as vscode.CodeActionContext;

		const actions = provider.provideCodeActions(doc, range, ctx);
		expect(actions).toHaveLength(1);
		expect(actions[0]?.title).toBe("Dismiss this error");
		expect(actions[0]?.command?.command).toBe("gaffer.dismissDiagnostic");
		expect(actions[0]?.command?.arguments).toEqual([uri]);
		// Action carries the diagnostic so VS Code can offer it
		// from the diagnostic's lightbulb.
		expect(actions[0]?.diagnostics).toEqual([gafferDiag]);
	});

	it("DismissDiagnosticActionProvider returns nothing when no gaffer diagnostics", () => {
		const provider = new DismissDiagnosticActionProvider();
		const uri = vscode.Uri.file("/p/projection.js");
		const doc = { uri } as vscode.TextDocument;
		const range = new vscode.Range(0, 0, 0, 1);
		const ctx = {
			diagnostics: [],
			only: undefined,
			triggerKind: 1,
		} as unknown as vscode.CodeActionContext;
		expect(provider.provideCodeActions(doc, range, ctx)).toEqual([]);
	});

	it("clearDiagnostics empties the collection", () => {
		initDiagnostics(makeContext());
		reportFatalError({
			file: "/p/projection.js",
			line: 1,
			column: 1,
			code: "JS_ERROR",
			description: "boom",
			jsStack: undefined,
			eventId: undefined,
		});
		expect(getState().diagnosticCollections[0]?.entries.size).toBe(1);
		clearDiagnostics();
		expect(getState().diagnosticCollections[0]?.entries.size).toBe(0);
	});
});
