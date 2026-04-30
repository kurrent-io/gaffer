import * as vscode from "vscode";
import * as v from "valibot";
import { jsonToTreeItems, type TreeItemWithChildren } from "./json-tree.js";
import { PartitionStateResponseSchema, type StateBody } from "../../types.js";

export class StateProvider implements vscode.TreeDataProvider<TreeItemWithChildren> {
	readonly #onDidChange = new vscode.EventEmitter<void>();
	readonly onDidChangeTreeData = this.#onDidChange.event;

	#state: StateBody | null = null;
	#debugSession: vscode.DebugSession | null = null;
	#refreshTimer: NodeJS.Timeout | null = null;

	clear(): void {
		this.#state = null;
		this.#debugSession = null;
		if (this.#refreshTimer) {
			clearTimeout(this.#refreshTimer);
			this.#refreshTimer = null;
		}
		this.#onDidChange.fire();
	}

	setDebugSession(session: vscode.DebugSession): void {
		this.#debugSession = session;
	}

	updateFromState(stateMsg: StateBody): void {
		this.#state = stateMsg;
		this.#scheduleRefresh();
	}

	getTreeItem(element: TreeItemWithChildren): vscode.TreeItem {
		return element;
	}

	async getChildren(
		element?: TreeItemWithChildren,
	): Promise<TreeItemWithChildren[]> {
		if (element) {
			if (element.contextValue === "partition") {
				if (!this.#debugSession) {
					return [
						new vscode.TreeItem(
							"No active session",
							vscode.TreeItemCollapsibleState.None,
						),
					];
				}
				const label =
					typeof element.label === "string"
						? element.label
						: (element.label?.label ?? "");
				return this.#fetchPartitionState(this.#debugSession, label);
			}
			return element.children ?? [];
		}
		if (!this.#state) {
			const empty = new vscode.TreeItem(
				"No state yet",
				vscode.TreeItemCollapsibleState.None,
			);
			empty.iconPath = new vscode.ThemeIcon("info");
			return [empty];
		}

		const items: TreeItemWithChildren[] = [];
		const s = this.#state;

		if (s.state) items.push(buildSection("state", "symbol-variable", s.state));
		if (s.result)
			items.push(buildSection("result", "symbol-variable", s.result));
		if (s.sharedState)
			items.push(
				buildSection("shared state", "symbol-variable", s.sharedState),
			);

		if (s.partitions?.length) {
			for (const name of s.partitions) {
				const partItem = new vscode.TreeItem(
					name,
					vscode.TreeItemCollapsibleState.Collapsed,
				);
				partItem.iconPath = new vscode.ThemeIcon("symbol-namespace");
				partItem.contextValue = "partition";
				items.push(partItem);
			}
		}

		return items;
	}

	#scheduleRefresh(): void {
		if (this.#refreshTimer) return;
		this.#refreshTimer = setTimeout(() => {
			this.#refreshTimer = null;
			this.#onDidChange.fire();
		}, 50);
	}

	async #fetchPartitionState(
		session: vscode.DebugSession,
		partition: string,
	): Promise<TreeItemWithChildren[]> {
		let raw: unknown;
		try {
			raw = await session.customRequest("gaffer/partitionState", {
				partition,
			});
		} catch {
			return [
				new vscode.TreeItem(
					"Failed to load",
					vscode.TreeItemCollapsibleState.None,
				),
			];
		}
		const parsed = v.safeParse(PartitionStateResponseSchema, raw);
		if (!parsed.success) {
			return [
				new vscode.TreeItem(
					"Failed to load (malformed response)",
					vscode.TreeItemCollapsibleState.None,
				),
			];
		}
		const body = parsed.output;
		const items: TreeItemWithChildren[] = [];
		if (body.state) items.push(buildSection("state", undefined, body.state));
		if (body.result) items.push(buildSection("result", undefined, body.result));
		if (items.length === 0) {
			items.push(
				new vscode.TreeItem("(empty)", vscode.TreeItemCollapsibleState.None),
			);
		}
		return items;
	}
}

function buildSection(
	label: string,
	icon: string | undefined,
	value: unknown,
): TreeItemWithChildren {
	const item: TreeItemWithChildren = new vscode.TreeItem(
		label,
		vscode.TreeItemCollapsibleState.Expanded,
	);
	if (icon) item.iconPath = new vscode.ThemeIcon(icon);
	item.children = jsonToTreeItems(value);
	return item;
}
