import * as vscode from "vscode";
import { jsonToTreeItems, type TreeItemWithChildren } from "./json-tree.js";
import type { EmittedEvent, InputEvent, StepResult } from "../ipc/schemas.js";

export class StepProvider
	implements vscode.TreeDataProvider<TreeItemWithChildren>, vscode.Disposable
{
	readonly #onDidChange = new vscode.EventEmitter<void>();
	readonly onDidChangeTreeData = this.#onDidChange.event;

	#items: TreeItemWithChildren[] = [];
	#refreshTimer: NodeJS.Timeout | null = null;

	dispose(): void {
		if (this.#refreshTimer) {
			clearTimeout(this.#refreshTimer);
			this.#refreshTimer = null;
		}
		this.#onDidChange.dispose();
	}

	clear(): void {
		this.#items = [];
		if (this.#refreshTimer) {
			clearTimeout(this.#refreshTimer);
			this.#refreshTimer = null;
		}
		this.#onDidChange.fire();
	}

	startStep(event: InputEvent): void {
		this.#items = [buildInputItem(event)];
		this.#onDidChange.fire();
	}

	addLog(message: string): void {
		this.#items.push(buildLogItem(message));
		this.#scheduleRefresh();
	}

	addEmit(emitData: EmittedEvent): void {
		this.#items.push(buildEmitItem(emitData));
		this.#scheduleRefresh();
	}

	addWarning(code: string, message: string): void {
		this.#items.push(buildWarningItem(code, message));
		this.#scheduleRefresh();
	}

	setResult(result: StepResult): void {
		this.#items.push(buildResultItem(result));
		this.#scheduleRefresh();
	}

	setError(code: string, description: string): void {
		const item = new vscode.TreeItem(
			code,
			vscode.TreeItemCollapsibleState.None,
		);
		item.iconPath = new vscode.ThemeIcon(
			"error",
			new vscode.ThemeColor("testing.iconFailed"),
		);
		item.description = description;
		this.#items.push(item);
		this.#onDidChange.fire();
	}

	getTreeItem(element: TreeItemWithChildren): vscode.TreeItem {
		return element;
	}

	getChildren(element?: TreeItemWithChildren): TreeItemWithChildren[] {
		if (element) return element.children ?? [];
		if (this.#items.length === 0) {
			// With --start-paused-if-no-breakpoints the user lands here on
			// click-Debug; the placeholder is the primary cue for what to
			// do next. Lead with the action ("Press Continue") rather than
			// the passive state ("Waiting for an event").
			const placeholder = new vscode.TreeItem(
				"Press Continue to start, or set breakpoints to pause at specific events.",
				vscode.TreeItemCollapsibleState.None,
			);
			placeholder.iconPath = new vscode.ThemeIcon("info");
			return [placeholder];
		}
		return this.#items;
	}

	#scheduleRefresh(): void {
		if (this.#refreshTimer) return;
		this.#refreshTimer = setTimeout(() => {
			this.#refreshTimer = null;
			this.#onDidChange.fire();
		}, 50);
	}
}

function buildInputItem(event: InputEvent): TreeItemWithChildren {
	const label = `${event.sequenceNumber}@${event.streamId}`;
	const item: TreeItemWithChildren = new vscode.TreeItem(
		label,
		vscode.TreeItemCollapsibleState.Expanded,
	);
	item.description = event.eventType;
	item.iconPath = new vscode.ThemeIcon("rocket");

	const children: TreeItemWithChildren[] = [];
	children.push(leaf("type", event.eventType));
	children.push(leaf("stream", event.streamId));
	children.push(leaf("revision", String(event.sequenceNumber)));

	if (hasValue(event.data)) children.push(buildNested("data", event.data));
	if (hasValue(event.metadata))
		children.push(buildNested("metadata", event.metadata));

	item.children = children;
	return item;
}

function buildLogItem(message: string): TreeItemWithChildren {
	const item: TreeItemWithChildren = new vscode.TreeItem(
		message,
		vscode.TreeItemCollapsibleState.None,
	);
	item.iconPath = new vscode.ThemeIcon("comment");
	return item;
}

function buildWarningItem(code: string, message: string): TreeItemWithChildren {
	const item: TreeItemWithChildren = new vscode.TreeItem(
		code,
		vscode.TreeItemCollapsibleState.None,
	);
	item.iconPath = new vscode.ThemeIcon(
		"warning",
		new vscode.ThemeColor("problemsWarningIcon.foreground"),
	);
	item.description = message;
	item.tooltip = message;
	return item;
}

function buildEmitItem(em: EmittedEvent): TreeItemWithChildren {
	const isLink = em.isLink ?? false;
	const item: TreeItemWithChildren = new vscode.TreeItem(
		em.streamId,
		vscode.TreeItemCollapsibleState.Collapsed,
	);
	item.description = isLink ? "link" : (em.eventType ?? "");
	item.iconPath = new vscode.ThemeIcon(isLink ? "link" : "forward");

	const children: TreeItemWithChildren[] = [];
	if (!isLink && em.eventType) children.push(leaf("type", em.eventType));
	if (hasValue(em.data)) children.push(buildNested("data", em.data));
	if (hasValue(em.metadata))
		children.push(buildNested("metadata", em.metadata));

	item.children = children;
	if (children.length === 0)
		item.collapsibleState = vscode.TreeItemCollapsibleState.None;
	return item;
}

function buildResultItem(result: StepResult): TreeItemWithChildren {
	if (result.status === "processed") {
		const desc = result.partition ? `[${result.partition}]` : "";
		const item: TreeItemWithChildren = new vscode.TreeItem(
			"processed",
			result.state
				? vscode.TreeItemCollapsibleState.Collapsed
				: vscode.TreeItemCollapsibleState.None,
		);
		item.iconPath = new vscode.ThemeIcon("pass");
		item.description = desc;

		if (result.state) {
			const children: TreeItemWithChildren[] = [];
			if (result.partition) children.push(leaf("partition", result.partition));
			children.push(buildNested("state", result.state));
			item.children = children;
		}
		return item;
	}

	const item = new vscode.TreeItem(
		"skipped",
		vscode.TreeItemCollapsibleState.None,
	);
	item.iconPath = new vscode.ThemeIcon("skip");
	item.description = result.reason;
	return item;
}

// Treat null and undefined as "absent" for event payload fields - a literal
// null `data`/`metadata` isn't worth a "null" node. Falsy values (0, "", false)
// inside actual state are still rendered by jsonToTreeItems.
function hasValue(v: unknown): boolean {
	return v !== undefined && v !== null;
}

function buildNested(label: string, value: unknown): TreeItemWithChildren {
	const item: TreeItemWithChildren = new vscode.TreeItem(
		label,
		vscode.TreeItemCollapsibleState.Collapsed,
	);
	item.children = jsonToTreeItems(value);
	return item;
}

function leaf(label: string, value: string): TreeItemWithChildren {
	const item: TreeItemWithChildren = new vscode.TreeItem(
		label,
		vscode.TreeItemCollapsibleState.None,
	);
	item.description = value;
	return item;
}
