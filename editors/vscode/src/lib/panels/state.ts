import * as vscode from "vscode";
import { jsonToTreeItems, type TreeItemWithChildren } from "./json-tree.js";
import type { PartitionStateResponse, StateBody } from "../../types.js";

export class StateProvider implements vscode.TreeDataProvider<TreeItemWithChildren> {
	private readonly _onDidChange = new vscode.EventEmitter<void>();
	readonly onDidChangeTreeData = this._onDidChange.event;

	private _state: StateBody | null = null;
	private _debugSession: vscode.DebugSession | null = null;
	private _refreshTimer: NodeJS.Timeout | null = null;

	clear(): void {
		this._state = null;
		this._debugSession = null;
		if (this._refreshTimer) {
			clearTimeout(this._refreshTimer);
			this._refreshTimer = null;
		}
		this._onDidChange.fire();
	}

	setDebugSession(session: vscode.DebugSession): void {
		this._debugSession = session;
	}

	updateFromState(stateMsg: StateBody): void {
		this._state = stateMsg;
		this._scheduleRefresh();
	}

	getTreeItem(element: TreeItemWithChildren): vscode.TreeItem {
		return element;
	}

	async getChildren(
		element?: TreeItemWithChildren,
	): Promise<TreeItemWithChildren[]> {
		if (element) {
			if (element.contextValue === "partition") {
				if (!this._debugSession) {
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
				return this._fetchPartitionState(label);
			}
			return element.children ?? [];
		}
		if (!this._state) {
			const empty = new vscode.TreeItem(
				"No state yet",
				vscode.TreeItemCollapsibleState.None,
			);
			empty.iconPath = new vscode.ThemeIcon("info");
			return [empty];
		}

		const items: TreeItemWithChildren[] = [];
		const s = this._state;

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

	private _scheduleRefresh(): void {
		if (this._refreshTimer) return;
		this._refreshTimer = setTimeout(() => {
			this._refreshTimer = null;
			this._onDidChange.fire();
		}, 50);
	}

	private async _fetchPartitionState(
		partition: string,
	): Promise<TreeItemWithChildren[]> {
		try {
			const body = (await this._debugSession!.customRequest(
				"gaffer/partitionState",
				{ partition },
			)) as PartitionStateResponse;

			const items: TreeItemWithChildren[] = [];
			if (body.state) items.push(buildSection("state", undefined, body.state));
			if (body.result)
				items.push(buildSection("result", undefined, body.result));
			if (items.length === 0) {
				items.push(
					new vscode.TreeItem("(empty)", vscode.TreeItemCollapsibleState.None),
				);
			}
			return items;
		} catch {
			return [
				new vscode.TreeItem(
					"Failed to load",
					vscode.TreeItemCollapsibleState.None,
				),
			];
		}
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
