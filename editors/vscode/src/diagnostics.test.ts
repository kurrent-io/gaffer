import { describe, expect, it } from "vitest";
import {
	clearDiagnostics,
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
