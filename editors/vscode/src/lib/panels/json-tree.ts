import * as vscode from "vscode";

export interface TreeItemWithChildren extends vscode.TreeItem {
	children?: vscode.TreeItem[];
}

export function jsonToTreeItems(value: unknown): TreeItemWithChildren[] {
	if (value === null || value === undefined) return [];
	if (typeof value !== "object") {
		return [new vscode.TreeItem(String(value), vscode.TreeItemCollapsibleState.None)];
	}
	if (Array.isArray(value)) {
		return value.map((v, i) => makeChild(`[${i}]`, v));
	}
	return Object.entries(value as Record<string, unknown>).map(([key, val]) =>
		makeChild(key, val),
	);
}

function makeChild(label: string, val: unknown): TreeItemWithChildren {
	if (typeof val === "object" && val !== null) {
		const item: TreeItemWithChildren = new vscode.TreeItem(
			label,
			vscode.TreeItemCollapsibleState.Collapsed,
		);
		item.children = jsonToTreeItems(val);
		return item;
	}
	const item: TreeItemWithChildren = new vscode.TreeItem(
		label,
		vscode.TreeItemCollapsibleState.None,
	);
	item.description = String(val);
	return item;
}
