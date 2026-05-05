import * as vscode from "vscode";
import { describe, expect, it } from "vitest";
import { JsCodeLensProvider } from "./js-provider.js";
import { makeDoc, okManifest } from "../../test/testutil/fixtures.js";
import type { ProjectIndex } from "../discovery/project-index.js";

function indexWith(
	entry: string,
	tomlDir: string,
	projectionName: string,
	fixtures: ReadonlyArray<{ name: string; path: string }> = [],
): ProjectIndex {
	return {
		lookup: (p) =>
			p === entry
				? {
						name: projectionName,
						tomlDir,
						fixtures: [...fixtures],
						invalidFixtures: [],
					}
				: null,
		size: 1,
		entryPaths: [entry],
		projections: [
			{
				name: projectionName,
				tomlUri: vscode.Uri.file(`${tomlDir}/gaffer.toml`),
				fixtures,
				invalidFixtures: [],
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

		it("matches `fromAll()` after preceding non-matching lines", () => {
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

	describe("fixture dropdown lens", () => {
		it("renders only the live lens when the projection has no fixtures", () => {
			const provider = new JsCodeLensProvider(
				indexWith("/p/app/a.js", "/p/app", "checkout"),
				okManifest,
			);
			const doc = makeDoc(vscode.Uri.file("/p/app/a.js"), "fromAll().when({})");
			const lenses = provider.provideCodeLenses(doc);
			expect(lenses).toHaveLength(1);
			expect(lenses[0]?.command?.command).toBe("gaffer.debugProjection");
		});

		it("renders both the live and fixture-pick lenses when fixtures exist", () => {
			const provider = new JsCodeLensProvider(
				indexWith("/p/app/a.js", "/p/app", "checkout", [
					{ name: "happy", path: "fixtures/happy.json" },
					{ name: "full", path: "fixtures/full.json" },
				]),
				okManifest,
			);
			const doc = makeDoc(vscode.Uri.file("/p/app/a.js"), "fromAll().when({})");
			const lenses = provider.provideCodeLenses(doc);
			expect(lenses).toHaveLength(2);
			expect(lenses.map((l) => l.command?.command)).toEqual([
				"gaffer.debugProjection",
				"gaffer.debugProjectionPick",
			]);
		});

		it("passes the projection name, tomlUri, and fixture names to the pick command", () => {
			const provider = new JsCodeLensProvider(
				indexWith("/p/app/a.js", "/p/app", "checkout", [
					{ name: "happy", path: "fixtures/happy.json" },
					{ name: "full", path: "fixtures/full.json" },
				]),
				okManifest,
			);
			const doc = makeDoc(vscode.Uri.file("/p/app/a.js"), "fromAll().when({})");
			const pickLens = provider
				.provideCodeLenses(doc)
				.find((l) => l.command?.command === "gaffer.debugProjectionPick");
			expect(pickLens).toBeDefined();
			const args = pickLens?.command?.arguments?.[0] as {
				name: string;
				tomlUri: vscode.Uri;
				fixtureNames: string[];
			};
			expect(args.name).toBe("checkout");
			expect(args.tomlUri.fsPath).toBe("/p/app/gaffer.toml");
			expect(args.fixtureNames).toEqual(["happy", "full"]);
		});

		it("hides the fixture-pick lens while the projection is being debugged live", () => {
			const provider = new JsCodeLensProvider(
				indexWith("/p/app/a.js", "/p/app", "checkout", [
					{ name: "happy", path: "fixtures/happy.json" },
				]),
				okManifest,
			);
			const doc = makeDoc(vscode.Uri.file("/p/app/a.js"), "fromAll().when({})");
			expect(provider.provideCodeLenses(doc)).toHaveLength(2);

			provider.setDebugState({ name: "checkout", status: "running" });
			const lenses = provider.provideCodeLenses(doc);
			expect(lenses).toHaveLength(1);
			expect(lenses[0]?.command?.command).toBe("gaffer.stopDebug");
		});

		it("restores the fixture-pick lens when the session ends", () => {
			// debugState.name stays set to the projection after the session
			// terminates (the projection-level lens relies on this to fall
			// back to a Debug button via stopTitle). The dropdown must use
			// the same status check or it stays hidden forever post-session.
			const provider = new JsCodeLensProvider(
				indexWith("/p/app/a.js", "/p/app", "checkout", [
					{ name: "happy", path: "fixtures/happy.json" },
				]),
				okManifest,
			);
			const doc = makeDoc(vscode.Uri.file("/p/app/a.js"), "fromAll().when({})");
			provider.setDebugState({ name: "checkout", status: "running" });
			expect(provider.provideCodeLenses(doc)).toHaveLength(1);

			provider.setDebugState({ name: "checkout", status: "ended" });
			const lenses = provider.provideCodeLenses(doc);
			expect(lenses).toHaveLength(2);
			expect(lenses.map((l) => l.command?.command)).toEqual([
				"gaffer.debugProjection",
				"gaffer.debugProjectionPick",
			]);
		});
	});
});
