import * as vscode from "vscode";
import { beforeEach, describe, expect, it } from "vitest";
import { TomlCodeLensProvider } from "./toml-provider.js";
import { initDiagnostics } from "../diagnostics.js";
import { makeDoc, okManifest } from "../../test/testutil/fixtures.js";
import { makeContext } from "../../test/testutil/fake-context.js";
import { getState } from "../../test/testutil/vscode-state.js";

function tomlDiagnostics(uri: vscode.Uri): vscode.Diagnostic[] {
	const c = getState().diagnosticCollections.find(
		(d) => d.name === "gaffer-toml",
	);
	return c?.entries.get(uri.fsPath) ?? [];
}

describe("TomlCodeLensProvider.provideCodeLenses", () => {
	beforeEach(() => {
		initDiagnostics(makeContext());
	});

	it("returns one lens per [[projection]] header line", () => {
		const provider = new TomlCodeLensProvider(okManifest);
		const doc = makeDoc(
			vscode.Uri.file("/p/gaffer.toml"),
			[
				"[[projection]]",
				'name = "a"',
				'entry = "a.js"',
				"",
				"[[projection]]",
				'name = "b"',
				'entry = "b.js"',
			].join("\n"),
		);
		const lenses = provider.provideCodeLenses(doc);
		expect(lenses).toHaveLength(2);
		expect(lenses[0]?.range.start.line).toBe(0);
		expect(lenses[1]?.range.start.line).toBe(4);
	});

	it("matches headers with leading whitespace and trailing comments", () => {
		const provider = new TomlCodeLensProvider(okManifest);
		const doc = makeDoc(
			vscode.Uri.file("/p/gaffer.toml"),
			["  [[projection]] # main", 'name = "a"', 'entry = "a.js"'].join("\n"),
		);
		const lenses = provider.provideCodeLenses(doc);
		expect(lenses).toHaveLength(1);
	});

	it("returns [] when there are no [[projection]] blocks", () => {
		const provider = new TomlCodeLensProvider(okManifest);
		const doc = makeDoc(vscode.Uri.file("/p/gaffer.toml"), `[other]\nx = 1\n`);
		expect(provider.provideCodeLenses(doc)).toEqual([]);
	});

	it("skips null block slots while keeping aligned headers", () => {
		// Two [[projection]] headers, second block is missing entry so
		// projectionBlocks returns [valid, null]. Headers count matches
		// blocks count, so we proceed and `if (!block) continue;` skips.
		const provider = new TomlCodeLensProvider(okManifest);
		const doc = makeDoc(
			vscode.Uri.file("/p/gaffer.toml"),
			[
				"[[projection]]",
				'name = "a"',
				'entry = "a.js"',
				"",
				"[[projection]]",
				'description = "no name or entry"',
			].join("\n"),
		);
		const lenses = provider.provideCodeLenses(doc);
		expect(lenses).toHaveLength(1);
		expect(lenses[0]?.range.start.line).toBe(0);
	});

	it("reflects a new manifest by changing the lens it produces", () => {
		const provider = new TomlCodeLensProvider(null);
		const doc = makeDoc(
			vscode.Uri.file("/p/gaffer.toml"),
			["[[projection]]", 'name = "a"', 'entry = "a.js"'].join("\n"),
		);
		expect(provider.provideCodeLenses(doc)).toEqual([]);
		provider.setManifest(okManifest);
		const lenses = provider.provideCodeLenses(doc);
		expect(lenses).toHaveLength(1);
		expect(lenses[0]?.command?.command).toBe("gaffer.debugProjection");
	});

	it("renders a Debug lens per valid fixtures.<name> = path entry, plus a dropdown on the projection line", () => {
		const provider = new TomlCodeLensProvider(okManifest);
		const doc = makeDoc(
			vscode.Uri.file("/p/gaffer.toml"),
			[
				"[[projection]]",
				'name = "a"',
				'entry = "a.js"',
				'fixtures.happy = "fixtures/happy.json"',
				'fixtures.edge = "fixtures/edge.json"',
			].join("\n"),
		);
		const lenses = provider.provideCodeLenses(doc);
		// projection Debug + projection dropdown + 2 per-fixture Debug
		expect(lenses).toHaveLength(4);
		expect(lenses[0]?.command?.command).toBe("gaffer.debugProjection");
		expect(lenses[0]?.command?.arguments?.[0]).not.toHaveProperty("fixture");
		expect(lenses[1]?.command?.command).toBe("gaffer.debugProjectionPick");
		// Dropdown names are alphabetical (matches CLI completion + JSON).
		expect(lenses[1]?.command?.arguments?.[0]).toMatchObject({
			name: "a",
			fixtureNames: ["edge", "happy"],
		});
		// Per-fixture lenses follow source line order, not alphabetical -
		// each lens is anchored to its declaration line.
		expect(lenses[2]?.command?.arguments?.[0]).toMatchObject({
			name: "a",
			fixture: "happy",
		});
		expect(lenses[3]?.command?.arguments?.[0]).toMatchObject({
			name: "a",
			fixture: "edge",
		});
	});

	it("renders a warning lens and emits a diagnostic for a non-string fixture path", () => {
		// Hostile or hand-malformed toml: `fixtures.x = 42`. Smol-toml
		// parses this as a number; we surface it both as a per-line
		// invalid-fixture lens and a Problems-panel diagnostic.
		const provider = new TomlCodeLensProvider(okManifest);
		const tomlUri = vscode.Uri.file("/p/gaffer.toml");
		const doc = makeDoc(
			tomlUri,
			[
				"[[projection]]",
				'name = "a"',
				'entry = "a.js"',
				"fixtures.broken = 42",
			].join("\n"),
		);
		const lenses = provider.provideCodeLenses(doc);
		// projection Debug + per-line warning lens (no dropdown - no
		// valid fixtures).
		expect(lenses).toHaveLength(2);
		expect(lenses[1]?.command?.title).toContain("Invalid fixture");
		expect(lenses[1]?.command?.title).toContain("path must be a string");

		const diags = tomlDiagnostics(tomlUri);
		expect(diags).toHaveLength(1);
		expect(diags[0]?.range.start.line).toBe(3);
		expect(diags[0]?.message).toContain("path must be a string");
		expect(diags[0]?.severity).toBe(vscode.DiagnosticSeverity.Error);
		expect(diags[0]?.source).toBe("gaffer");
	});

	it("emits diagnostics for invalid fixtures even when declared via inline-table form", () => {
		// Inline-table form: no per-fixture line to anchor a warning
		// lens against, so we fall back to the projection header range
		// for the diagnostic. Lens still falls back to the dropdown for
		// the valid entries; invalid ones get diagnostics-only.
		const provider = new TomlCodeLensProvider(okManifest);
		const tomlUri = vscode.Uri.file("/p/gaffer.toml");
		const doc = makeDoc(
			tomlUri,
			[
				"[[projection]]",
				'name = "a"',
				'entry = "a.js"',
				'fixtures = { happy = "fixtures/happy.json", evil = "../escape.json" }',
			].join("\n"),
		);
		provider.provideCodeLenses(doc);

		const diags = tomlDiagnostics(tomlUri);
		expect(diags).toHaveLength(1);
		expect(diags[0]?.range.start.line).toBe(0);
		expect(diags[0]?.message).toContain("evil");
		expect(diags[0]?.message).toContain("escapes");
	});

	it("clears toml diagnostics when the file becomes valid", () => {
		const provider = new TomlCodeLensProvider(okManifest);
		const tomlUri = vscode.Uri.file("/p/gaffer.toml");
		const badDoc = makeDoc(
			tomlUri,
			[
				"[[projection]]",
				'name = "a"',
				'entry = "a.js"',
				"fixtures.broken = 42",
			].join("\n"),
		);
		provider.provideCodeLenses(badDoc);
		expect(tomlDiagnostics(tomlUri)).toHaveLength(1);

		const goodDoc = makeDoc(
			tomlUri,
			["[[projection]]", 'name = "a"', 'entry = "a.js"'].join("\n"),
		);
		provider.provideCodeLenses(goodDoc);
		expect(tomlDiagnostics(tomlUri)).toEqual([]);
	});

	it("renders an error lens for a path that escapes the project root", () => {
		const provider = new TomlCodeLensProvider(okManifest);
		const doc = makeDoc(
			vscode.Uri.file("/p/gaffer.toml"),
			[
				"[[projection]]",
				'name = "a"',
				'entry = "a.js"',
				'fixtures.happy = "fixtures/happy.json"',
				'fixtures.evil = "../escape.json"',
			].join("\n"),
		);
		const lenses = provider.provideCodeLenses(doc);
		// projection Debug + dropdown + happy Debug + evil error lens
		expect(lenses).toHaveLength(4);
		const escapeLens = lenses[3];
		expect(escapeLens?.command?.title).toContain("Invalid fixture");
		expect(escapeLens?.command?.title).toContain("escapes");
		expect(escapeLens?.command?.command).toBe("gaffer.noop");
	});

	it("excludes invalid fixtures from the inline-table dropdown", () => {
		// Inline-table mixing valid + invalid: the dropdown should
		// only contain valid fixture names so a click can't end up
		// running a fixture the CLI would reject.
		const provider = new TomlCodeLensProvider(okManifest);
		const doc = makeDoc(
			vscode.Uri.file("/p/gaffer.toml"),
			[
				"[[projection]]",
				'name = "a"',
				'entry = "a.js"',
				'fixtures = { happy = "fixtures/happy.json", evil = "../escape.json" }',
			].join("\n"),
		);
		const lenses = provider.provideCodeLenses(doc);
		const pickLens = lenses.find(
			(l) => l.command?.command === "gaffer.debugProjectionPick",
		);
		const args = pickLens?.command?.arguments?.[0] as
			| { fixtureNames: string[] }
			| undefined;
		expect(args?.fixtureNames).toEqual(["happy"]);
	});

	it("renders only the dropdown when fixtures use inline-table form (no per-line lenses possible)", () => {
		// Inline-table form: smol-toml parses the fixtures, but the
		// scanner can't put a lens on each one because there are no
		// `fixtures.<name>` lines. The projection-line dropdown is the
		// fallback so the fixtures are still reachable.
		const provider = new TomlCodeLensProvider(okManifest);
		const doc = makeDoc(
			vscode.Uri.file("/p/gaffer.toml"),
			[
				"[[projection]]",
				'name = "a"',
				'entry = "a.js"',
				'fixtures = { happy = "fixtures/happy.json", edge = "fixtures/edge.json" }',
			].join("\n"),
		);
		const lenses = provider.provideCodeLenses(doc);
		expect(lenses).toHaveLength(2);
		expect(lenses[0]?.command?.command).toBe("gaffer.debugProjection");
		expect(lenses[1]?.command?.command).toBe("gaffer.debugProjectionPick");
		expect(lenses[1]?.command?.arguments?.[0]).toMatchObject({
			name: "a",
			fixtureNames: ["edge", "happy"],
		});
	});

	it("does not swap a fixture lens to Stop when its parent projection is debugging", () => {
		// The projection-level lens swaps to Stop and the dropdown hides
		// (mirrors the JS file). Per-fixture lenses stay clickable so the
		// user can switch fixtures mid-session.
		const provider = new TomlCodeLensProvider(okManifest);
		provider.setDebugState({ name: "a", status: "running" });
		const doc = makeDoc(
			vscode.Uri.file("/p/gaffer.toml"),
			[
				"[[projection]]",
				'name = "a"',
				'entry = "a.js"',
				'fixtures.happy = "fixtures/happy.json"',
			].join("\n"),
		);
		const lenses = provider.provideCodeLenses(doc);
		expect(lenses).toHaveLength(2);
		expect(lenses[0]?.command?.command).toBe("gaffer.stopDebug");
		expect(lenses[1]?.command?.command).toBe("gaffer.debugProjection");
	});

	it("matches fixture lines with whitespace around the dotted key", () => {
		// TOML grammar permits whitespace around dots in dotted keys.
		// Scanner over-matches to stay aligned with smol-toml's parser.
		const provider = new TomlCodeLensProvider(okManifest);
		const doc = makeDoc(
			vscode.Uri.file("/p/gaffer.toml"),
			[
				"[[projection]]",
				'name = "a"',
				'entry = "a.js"',
				'  fixtures . happy   = "fixtures/happy.json"',
			].join("\n"),
		);
		const lenses = provider.provideCodeLenses(doc);
		// projection Debug + dropdown + happy Debug
		expect(lenses).toHaveLength(3);
		expect(lenses[2]?.command?.arguments?.[0]).toMatchObject({
			name: "a",
			fixture: "happy",
		});
	});

	it("drops fixture lines attached to a malformed [[projection]] block", () => {
		// First projection is missing name/entry so its block slot is
		// null; fixtures attached to that block are silently dropped at
		// lens-render time even though smol-toml has parsed them.
		const provider = new TomlCodeLensProvider(okManifest);
		const doc = makeDoc(
			vscode.Uri.file("/p/gaffer.toml"),
			[
				"[[projection]]",
				'description = "no name or entry"',
				'fixtures.orphan = "fixtures/orphan.json"',
				"",
				"[[projection]]",
				'name = "b"',
				'entry = "b.js"',
			].join("\n"),
		);
		const lenses = provider.provideCodeLenses(doc);
		expect(lenses).toHaveLength(1);
		expect(lenses[0]?.command?.arguments?.[0]).toMatchObject({ name: "b" });
		expect(lenses[0]?.command?.arguments?.[0]).not.toHaveProperty("fixture");
	});

	it("reflects a new debug state by switching the lens to a Stop button", () => {
		const provider = new TomlCodeLensProvider(okManifest);
		const doc = makeDoc(
			vscode.Uri.file("/p/gaffer.toml"),
			["[[projection]]", 'name = "a"', 'entry = "a.js"'].join("\n"),
		);
		expect(provider.provideCodeLenses(doc)[0]?.command?.command).toBe(
			"gaffer.debugProjection",
		);

		provider.setDebugState({ name: "a", status: "running" });
		expect(provider.provideCodeLenses(doc)[0]?.command?.command).toBe(
			"gaffer.stopDebug",
		);
	});
});
