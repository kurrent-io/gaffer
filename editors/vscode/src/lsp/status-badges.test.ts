import * as vscode from "vscode";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
	buildHealthRowSvg,
	StatusBadges,
	type BadgeCell,
} from "./status-badges.js";
import {
	type FakeDecoration,
	makeFakeTextEditor,
} from "../../test/__mocks__/vscode.js";
import {
	fireVisibleEditorsChanged,
	getDecorationTypes,
	resetVscode,
	setVisibleTextEditors,
} from "../../test/testutil/vscode-state.js";

const TOML = "file:///ws/gaffer.toml";

function doc(uri: string): vscode.TextDocument {
	return { uri: vscode.Uri.parse(uri) } as vscode.TextDocument;
}

function cell(line: number, healths: BadgeCell["healths"]): BadgeCell {
	return { range: new vscode.Range(line, 0, line, 0), healths };
}

// The single decoration type's key (there's exactly one).
function decoKey(): string {
	const types = getDecorationTypes();
	if (types.length !== 1)
		throw new Error(`expected 1 type, got ${types.length}`);
	return types[0]?.key ?? "";
}

function decorationsOn(
	editor: ReturnType<typeof makeFakeTextEditor>,
): readonly FakeDecoration[] {
	return editor.decorations.get(decoKey()) ?? [];
}

describe("buildHealthRowSvg", () => {
	it("draws one filled circle per known health, in order", () => {
		const svg = buildHealthRowSvg(["green", "orange", "red"]);
		const circles = svg.match(/<circle/g) ?? [];
		expect(circles).toHaveLength(3);
		expect(svg).toContain('fill="#3fb950"');
		expect(svg).toContain('fill="#d29922"');
		expect(svg).toContain('fill="#f85149"');
	});

	it("draws locked as a hollow ring", () => {
		const svg = buildHealthRowSvg(["locked"]);
		expect(svg).toContain('fill="none"');
		expect(svg).toContain("stroke=");
		expect(svg).not.toContain("<line"); // no slash
	});

	it("draws error as a hollow ring with a slash", () => {
		const svg = buildHealthRowSvg(["error"]);
		expect(svg).toContain('fill="none"');
		expect(svg).toContain("<line");
	});

	it("draws loading as a faint filled dot", () => {
		const svg = buildHealthRowSvg(["loading"]);
		expect(svg).toContain("opacity=");
		expect(svg).not.toContain("<line");
	});

	it("widens the viewBox with the env count", () => {
		const one = buildHealthRowSvg(["green"]);
		const three = buildHealthRowSvg(["green", "green", "green"]);
		const widthOf = (s: string) => Number(/viewBox="0 0 (\d+)/.exec(s)?.[1]);
		expect(widthOf(three)).toBe(widthOf(one) * 3);
	});
});

describe("StatusBadges", () => {
	beforeEach(() => resetVscode());
	afterEach(() => resetVscode());

	it("creates a single decoration type and disposes it", () => {
		const g = new StatusBadges();
		expect(getDecorationTypes()).toHaveLength(1);
		g.dispose();
		expect(getDecorationTypes().every((t) => t.disposed)).toBe(true);
	});

	it("paints an SVG badge row after each projection header", () => {
		const g = new StatusBadges();
		const editor = makeFakeTextEditor(doc(TOML));
		setVisibleTextEditors([editor]);

		g.set(vscode.Uri.parse(TOML), [
			cell(4, ["green", "orange"]),
			cell(8, ["red", "locked", "green"]),
		]);

		const decos = decorationsOn(editor);
		expect(decos.map((d) => d.line)).toEqual([4, 8]);
		for (const d of decos) {
			expect(d.afterIcon).toMatch(/^data:image\/svg\+xml;base64,/);
		}
	});

	it("does not paint an editor showing a different document", () => {
		const g = new StatusBadges();
		const other = makeFakeTextEditor(doc("file:///ws/other.toml"));
		setVisibleTextEditors([other]);
		g.set(vscode.Uri.parse(TOML), [cell(4, ["red"])]);
		expect(other.decorations.size).toBe(0);
	});

	it("re-applies cached cells when an editor becomes visible", () => {
		const g = new StatusBadges();
		// Status lands while no editor is visible.
		g.set(vscode.Uri.parse(TOML), [cell(8, ["green"])]);
		// The editor showing it appears later (tab switch / split).
		const editor = makeFakeTextEditor(doc(TOML));
		fireVisibleEditorsChanged([editor]);
		expect(decorationsOn(editor).map((d) => d.line)).toEqual([8]);
	});

	it("clears a document's markers when given empty cells", () => {
		const g = new StatusBadges();
		const editor = makeFakeTextEditor(doc(TOML));
		setVisibleTextEditors([editor]);
		g.set(vscode.Uri.parse(TOML), [cell(4, ["red"])]);
		expect(decorationsOn(editor)).toHaveLength(1);
		g.set(vscode.Uri.parse(TOML), []);
		expect(decorationsOn(editor)).toHaveLength(0);
	});
});
