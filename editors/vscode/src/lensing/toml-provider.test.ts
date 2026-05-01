import * as vscode from "vscode";
import { describe, expect, it } from "vitest";
import { TomlCodeLensProvider } from "./toml-provider.js";
import type { Manifest } from "../discovery/schemas.js";

function makeDoc(uri: vscode.Uri, text: string): vscode.TextDocument {
	return {
		uri,
		getText: () => text,
	} as unknown as vscode.TextDocument;
}

const okManifest: Manifest = {
	version: "1.0.0",
	commands: { dev: { flags: ["debug"] } },
};

describe("TomlCodeLensProvider.provideCodeLenses", () => {
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
