import * as vscode from "vscode";
import * as v from "valibot";
import { jsonToTreeItems, type TreeItemWithChildren } from "./json-tree.js";
import { log } from "../output.js";
import {
	PartitionStateResponseSchema,
	type StateBody,
	type PartitionStateResponse,
} from "../debugging/schemas.js";

const PARTITION_FETCH_TIMEOUT_MS = 5000;

class PartitionFetchTimeout extends Error {
	constructor(message: string) {
		super(message);
		this.name = "PartitionFetchTimeout";
	}
}

function withTimeout<T>(promise: Thenable<T>, ms: number): Promise<T> {
	let timer: NodeJS.Timeout;
	const timeout = new Promise<never>((_, reject) => {
		timer = setTimeout(
			() => reject(new PartitionFetchTimeout(`Timed out after ${ms}ms`)),
			ms,
		);
	});
	return Promise.race([Promise.resolve(promise), timeout]).finally(() => {
		clearTimeout(timer);
	});
}

export class StateProvider implements vscode.TreeDataProvider<TreeItemWithChildren> {
	readonly #onDidChange = new vscode.EventEmitter<void>();
	readonly onDidChangeTreeData = this.#onDidChange.event;

	#state: StateBody | null = null;
	#debugSession: vscode.DebugSession | null = null;
	#refreshTimer: NodeJS.Timeout | null = null;
	// Cache populated as the user expands partitions during a live
	// session; serves as the source of truth in post-mortem (ended
	// mode) when there's no live debug session to query. Latest-wins
	// during the session - the cache reflects "last known state".
	readonly #partitionCache = new Map<string, PartitionStateResponse>();
	// Set true by markEnded; gates writes (setDebugSession,
	// updateFromState) so late DAP events flushed after end don't
	// surprise the user with post-stop changes or resurrect a dead
	// session reference.
	#ended = false;

	clear(): void {
		this.#state = null;
		this.#debugSession = null;
		this.#partitionCache.clear();
		this.#ended = false;
		if (this.#refreshTimer) {
			clearTimeout(this.#refreshTimer);
			this.#refreshTimer = null;
		}
		this.#onDidChange.fire();
	}

	// Freeze for post-mortem inspection. Preserves #state and the
	// partition cache; nulls the live session ref so getChildren
	// serves cached partitions instead of attempting customRequest.
	markEnded(): void {
		this.#ended = true;
		this.#debugSession = null;
		this.#onDidChange.fire();
	}

	setDebugSession(session: vscode.DebugSession): void {
		if (this.#ended) return;
		this.#debugSession = session;
	}

	updateFromState(stateMsg: StateBody): void {
		if (this.#ended) return;
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
				const label =
					typeof element.label === "string"
						? element.label
						: (element.label?.label ?? "");
				return this.#fetchPartitionState(label);
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
		partition: string,
	): Promise<TreeItemWithChildren[]> {
		// Live: customRequest with timeout, populate cache, render.
		if (this.#debugSession) {
			let raw: unknown;
			try {
				raw = await withTimeout(
					this.#debugSession.customRequest("gaffer/partitionState", {
						partition,
					}),
					PARTITION_FETCH_TIMEOUT_MS,
				);
			} catch (err) {
				if (err instanceof PartitionFetchTimeout) {
					log(`Partition fetch timed out: ${partition}`);
					return [errorItem("Failed to load (timeout)")];
				}
				const msg = err instanceof Error ? err.message : String(err);
				log(`Partition fetch failed for ${partition}: ${msg}`);
				return [errorItem(`Failed to load: ${truncate(msg, 80)}`)];
			}
			const parsed = v.safeParse(PartitionStateResponseSchema, raw);
			if (!parsed.success) {
				log(
					`Malformed partition response for ${partition}: ${parsed.issues
						.map((i) => i.message)
						.join("; ")}`,
				);
				return [errorItem("Failed to load (malformed response)")];
			}
			this.#partitionCache.set(partition, parsed.output);
			return renderPartition(parsed.output);
		}
		// Post-mortem: serve from cache.
		const cached = this.#partitionCache.get(partition);
		if (cached) return renderPartition(cached);
		return [errorItem("(not loaded during session)")];
	}
}

function errorItem(label: string): TreeItemWithChildren {
	return new vscode.TreeItem(label, vscode.TreeItemCollapsibleState.None);
}

function truncate(s: string, max: number): string {
	return s.length > max ? `${s.slice(0, max)}…` : s;
}

function renderPartition(body: PartitionStateResponse): TreeItemWithChildren[] {
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
