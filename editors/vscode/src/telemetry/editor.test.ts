import { describe, expect, it } from "vitest";

import { detectEditor } from "./editor.js";

describe("detectEditor", () => {
	it.each([
		["Visual Studio Code", "vscode"],
		["Visual Studio Code - Insiders", "vscode"],
		["Cursor", "cursor"],
		["Cursor (Insiders)", "cursor"],
		["Windsurf", "windsurf"],
		["VSCodium", "vscodium"],
		["Code - OSS", "vscodium"],
		["JetBrains Fleet", "other"],
		["", "other"],
	])("maps %q -> %q", (appName, expected) => {
		expect(detectEditor(appName)).toBe(expected);
	});

	it("is case-insensitive", () => {
		expect(detectEditor("CURSOR")).toBe("cursor");
		expect(detectEditor("visual studio code")).toBe("vscode");
	});
});
