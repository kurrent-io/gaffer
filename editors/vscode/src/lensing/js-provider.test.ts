import * as vscode from "vscode";
import { describe, expect, it } from "vitest";
import { JsCodeLensProvider } from "./js-provider.js";
import { makeDoc, okManifest } from "../../test/testutil/fixtures.js";
import type { ProjectIndex } from "../discovery/project-index.js";

function indexWith(
	entry: string,
	tomlDir: string,
	projectionName: string,
): ProjectIndex {
	return {
		lookup: (p) => (p === entry ? { name: projectionName, tomlDir } : null),
		size: 1,
		entryPaths: [entry],
		projections: [
			{
				name: projectionName,
				tomlUri: vscode.Uri.file(`${tomlDir}/gaffer.toml`),
			},
		],
		projectRoot: tomlDir,
	};
}

const emptyIndex: ProjectIndex = {
	lookup: () => null,
	size: 0,
	entryPaths: [],
	projections: [],
	projectRoot: undefined,
};

describe("JsCodeLensProvider.provideCodeLenses", () => {
	it("returns [] when the document is not a registered entry", () => {
		const provider = new JsCodeLensProvider(emptyIndex, okManifest);
		const doc = makeDoc(vscode.Uri.file("/p/random.js"), "fromAll().when({})");
		expect(provider.provideCodeLenses(doc)).toEqual([]);
	});

	it("places a lens on the line that opens the from* call", () => {
		const provider = new JsCodeLensProvider(
			indexWith("/p/app/a.js", "/p/app", "checkout"),
			okManifest,
		);
		const text = ["// header", "", "fromAll()", "  .when({})"].join("\n");
		const doc = makeDoc(vscode.Uri.file("/p/app/a.js"), text);
		const lenses = provider.provideCodeLenses(doc);
		expect(lenses).toHaveLength(1);
		expect(lenses[0]?.range.start.line).toBe(2);
	});

	it("recognises fromStream, fromCategory, fromStreams", () => {
		const provider = new JsCodeLensProvider(
			indexWith("/p/app/a.js", "/p/app", "checkout"),
			okManifest,
		);
		for (const head of ["fromStream", "fromCategory", "fromStreams"]) {
			const text = `${head}("x").when({})`;
			const doc = makeDoc(vscode.Uri.file("/p/app/a.js"), text);
			const lenses = provider.provideCodeLenses(doc);
			expect(lenses).toHaveLength(1);
		}
	});

	it("returns [] when the source has no from* call", () => {
		const provider = new JsCodeLensProvider(
			indexWith("/p/app/a.js", "/p/app", "checkout"),
			okManifest,
		);
		const doc = makeDoc(vscode.Uri.file("/p/app/a.js"), "console.log(1)");
		expect(provider.provideCodeLenses(doc)).toEqual([]);
	});

	describe("regex specificity", () => {
		// The fromPattern regex in js-provider.ts is anchored with `^` and
		// tested against `line.trim()`. trim only strips whitespace, so
		// leading `//` or `*` prefixes survive and the anchor blocks the
		// match. Lock the negative cases in - a future regex change that
		// drops `^` would silently start lensing comment lines.
		const provider = (): JsCodeLensProvider =>
			new JsCodeLensProvider(
				indexWith("/p/app/a.js", "/p/app", "checkout"),
				okManifest,
			);
		const docOf = (text: string): vscode.TextDocument =>
			makeDoc(vscode.Uri.file("/p/app/a.js"), text);

		it("does not match a `// fromAll()` line comment", () => {
			expect(provider().provideCodeLenses(docOf("// fromAll()"))).toEqual([]);
		});

		it("does not match an indented `// fromAll()` line comment", () => {
			expect(provider().provideCodeLenses(docOf("    // fromAll()"))).toEqual(
				[],
			);
		});

		it("does not match a `* fromAll()` JSDoc-style line", () => {
			expect(provider().provideCodeLenses(docOf(" * fromAll()"))).toEqual([]);
		});

		it("does not match a lookalike like `myFromStream(...)`", () => {
			expect(
				provider().provideCodeLenses(docOf("myFromStream('x').when({})")),
			).toEqual([]);
		});

		it("does match `fromAll()` after non-matching content", () => {
			const text = ["// header", "const x = 1;", "", "fromAll().when({})"].join(
				"\n",
			);
			const lenses = provider().provideCodeLenses(docOf(text));
			expect(lenses).toHaveLength(1);
			expect(lenses[0]?.range.start.line).toBe(3);
		});
	});

	it("reflects setIndex by switching from no-lens to a Debug lens", () => {
		const provider = new JsCodeLensProvider(emptyIndex, okManifest);
		const doc = makeDoc(vscode.Uri.file("/p/app/a.js"), "fromAll().when({})");
		expect(provider.provideCodeLenses(doc)).toEqual([]);
		provider.setIndex(indexWith("/p/app/a.js", "/p/app", "checkout"));
		const lenses = provider.provideCodeLenses(doc);
		expect(lenses).toHaveLength(1);
		expect(lenses[0]?.command?.command).toBe("gaffer.debugProjection");
	});

	it("flips the lens to a Stop button when debug state matches the projection", () => {
		const index = indexWith("/p/app/a.js", "/p/app", "checkout");
		const provider = new JsCodeLensProvider(index, okManifest);
		const doc = makeDoc(vscode.Uri.file("/p/app/a.js"), "fromAll().when({})");

		expect(provider.provideCodeLenses(doc)[0]?.command?.command).toBe(
			"gaffer.debugProjection",
		);

		provider.setDebugState({ name: "checkout", status: "running" });
		expect(provider.provideCodeLenses(doc)[0]?.command?.command).toBe(
			"gaffer.stopDebug",
		);
	});
});
