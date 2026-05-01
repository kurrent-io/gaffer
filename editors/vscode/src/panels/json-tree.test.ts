import * as vscode from "vscode";
import { describe, expect, it } from "vitest";
import { jsonToTreeItems } from "./json-tree.js";

describe("jsonToTreeItems", () => {
	it("returns [] for null and undefined", () => {
		expect(jsonToTreeItems(null)).toEqual([]);
		expect(jsonToTreeItems(undefined)).toEqual([]);
	});

	it("renders a primitive as a single leaf with its string form", () => {
		const items = jsonToTreeItems(42);
		expect(items).toHaveLength(1);
		expect(items[0]?.label).toBe("42");
		expect(items[0]?.collapsibleState).toBe(
			vscode.TreeItemCollapsibleState.None,
		);
	});

	it("renders falsy primitives (0, '', false) as leaves rather than empty", () => {
		expect(jsonToTreeItems(0)[0]?.label).toBe("0");
		expect(jsonToTreeItems("")[0]?.label).toBe("");
		expect(jsonToTreeItems(false)[0]?.label).toBe("false");
	});

	it("renders an array as `[i]`-labelled children in order", () => {
		const items = jsonToTreeItems(["a", "b"]);
		expect(items.map((i) => i.label)).toEqual(["[0]", "[1]"]);
		expect(items[0]?.description).toBe("a");
	});

	it("renders an object as key/value leaves", () => {
		const items = jsonToTreeItems({ name: "x", count: 3 });
		expect(items.map((i) => i.label)).toEqual(["name", "count"]);
		expect(items[0]?.description).toBe("x");
		expect(items[1]?.description).toBe("3");
	});

	it("uses Collapsed state for nested objects/arrays", () => {
		const items = jsonToTreeItems({ list: [1, 2] });
		expect(items[0]?.collapsibleState).toBe(
			vscode.TreeItemCollapsibleState.Collapsed,
		);
		const nested = (items[0] as { children?: unknown[] }).children ?? [];
		expect(nested).toHaveLength(2);
	});
});
