import * as vscode from "vscode";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { StateProvider } from "./state.js";
import { makePartitionElement } from "../../test/testutil/fixtures.js";
import type { StateBody } from "../debugging/schemas.js";

interface FakeDebugSession {
	customRequest: (cmd: string, args?: unknown) => Promise<unknown>;
}

function makeSession(
	impl: FakeDebugSession["customRequest"],
): FakeDebugSession {
	return { customRequest: impl };
}

describe("StateProvider", () => {
	beforeEach(() => {
		vi.useFakeTimers();
	});
	afterEach(() => {
		vi.useRealTimers();
	});

	describe("initial state", () => {
		it("renders a 'No state yet' placeholder when nothing has been loaded", async () => {
			const provider = new StateProvider();
			const items = await provider.getChildren();
			expect(items).toHaveLength(1);
			expect(items[0]?.label).toBe("No state yet");
		});
	});

	describe("updateFromState", () => {
		it("renders state, result, sharedState as separate sections", async () => {
			const provider = new StateProvider();
			const body: StateBody = {
				state: { count: 1 },
				result: { ok: true },
				sharedState: { x: "y" },
			};
			provider.updateFromState(body);
			vi.advanceTimersByTime(50);
			const items = await provider.getChildren();
			expect(items.map((i) => i.label)).toEqual([
				"state",
				"result",
				"shared state",
			]);
		});

		it("appends partition entries as collapsed children", async () => {
			const provider = new StateProvider();
			provider.updateFromState({
				state: { count: 1 },
				partitions: ["p1", "p2"],
			});
			vi.advanceTimersByTime(50);
			const items = await provider.getChildren();
			const partitionItems = items.filter(
				(i) => i.contextValue === "partition",
			);
			expect(partitionItems.map((i) => i.label)).toEqual(["p1", "p2"]);
			expect(partitionItems[0]?.collapsibleState).toBe(
				vscode.TreeItemCollapsibleState.Collapsed,
			);
		});

		it("debounces refresh fires within 50ms", () => {
			const provider = new StateProvider();
			let fires = 0;
			provider.onDidChangeTreeData(() => {
				fires++;
			});
			provider.updateFromState({ state: { v: 1 } });
			provider.updateFromState({ state: { v: 2 } });
			provider.updateFromState({ state: { v: 3 } });
			expect(fires).toBe(0);
			vi.advanceTimersByTime(50);
			expect(fires).toBe(1);
		});
	});

	describe("partition fetch (live session)", () => {
		it("issues customRequest with the partition name and renders the response", async () => {
			const provider = new StateProvider();
			const calls: Array<{ command: string; args: unknown }> = [];
			const session = makeSession(async (command, args) => {
				calls.push({ command, args });
				return { state: { count: 7 } };
			});
			provider.setDebugSession(session as unknown as vscode.DebugSession);
			const partition = makePartitionElement("p1");
			vi.useRealTimers();
			const items = await provider.getChildren(partition);
			expect(calls).toEqual([
				{ command: "gaffer/partitionState", args: { partition: "p1" } },
			]);
			expect(items.map((i) => i.label)).toEqual(["state"]);
		});

		it("returns an error item when customRequest rejects", async () => {
			const provider = new StateProvider();
			const session = makeSession(() => Promise.reject(new Error("boom")));
			provider.setDebugSession(session as unknown as vscode.DebugSession);
			const partition = makePartitionElement("p1");
			vi.useRealTimers();
			const items = await provider.getChildren(partition);
			expect(items[0]?.label).toMatch(/Failed to load: boom/);
		});

		it("returns an error item when customRequest resolves with malformed body", async () => {
			const provider = new StateProvider();
			const session = makeSession(async () => "not-an-object");
			provider.setDebugSession(session as unknown as vscode.DebugSession);
			const partition = makePartitionElement("p1");
			vi.useRealTimers();
			const items = await provider.getChildren(partition);
			expect(items[0]?.label).toMatch(/malformed response/);
		});

		it("returns a timeout error item when customRequest hangs past 5s", async () => {
			const provider = new StateProvider();
			const session = makeSession(() => new Promise(() => {}));
			provider.setDebugSession(session as unknown as vscode.DebugSession);
			const partition = makePartitionElement("p1");
			const fetch = provider.getChildren(partition);
			vi.advanceTimersByTime(5001);
			const items = await fetch;
			expect(items[0]?.label).toMatch(/timeout/i);
		});
	});

	describe("post-mortem (markEnded)", () => {
		it("preserves cached partition data after markEnded", async () => {
			const provider = new StateProvider();
			const session = makeSession(async () => ({ state: { count: 7 } }));
			provider.setDebugSession(session as unknown as vscode.DebugSession);
			const partition = makePartitionElement("p1");
			vi.useRealTimers();
			await provider.getChildren(partition);
			// End the session.
			provider.markEnded();
			const items = await provider.getChildren(partition);
			expect(items.map((i) => i.label)).toEqual(["state"]);
		});

		it("returns a 'not loaded during session' item for partitions never expanded live", async () => {
			const provider = new StateProvider();
			provider.markEnded();
			const partition = makePartitionElement("p1");
			const items = await provider.getChildren(partition);
			expect(items[0]?.label).toMatch(/not loaded/);
		});

		it("hydrateFinalState pre-populates the cache so post-mortem expansion shows real values", async () => {
			const provider = new StateProvider();
			provider.hydrateFinalState({
				partitions: {
					p1: { state: { count: 1 } },
					p2: { state: { count: 2 } },
				},
			});
			provider.markEnded();
			const items = await provider.getChildren(makePartitionElement("p2"));
			expect(items.map((i) => i.label)).toEqual(["state"]);
		});

		it("hydrateFinalState updates the partition list so the tree shows the final shape", async () => {
			const provider = new StateProvider();
			provider.hydrateFinalState({
				partitions: {
					p1: { state: { count: 1 } },
					p2: { state: { count: 2 } },
				},
			});
			vi.advanceTimersByTime(50);
			const items = await provider.getChildren();
			const partitions = items.filter((i) => i.contextValue === "partition");
			expect(partitions.map((i) => i.label).sort()).toEqual(["p1", "p2"]);
		});

		it("hydrateFinalState applies even when called after markEnded", async () => {
			const provider = new StateProvider();
			provider.markEnded();
			provider.hydrateFinalState({
				partitions: { p1: { state: { count: 99 } } },
			});
			const items = await provider.getChildren(makePartitionElement("p1"));
			expect(items.map((i) => i.label)).toEqual(["state"]);
		});

		it("ignores updateFromState after markEnded", async () => {
			const provider = new StateProvider();
			provider.updateFromState({ state: { v: 1 } });
			vi.advanceTimersByTime(50);
			provider.markEnded();
			provider.updateFromState({ state: { v: "post-mortem" } });
			vi.advanceTimersByTime(50);
			const items = await provider.getChildren();
			// Still rendering the pre-end state.
			const stateSection = items.find((i) => i.label === "state");
			const stateChildren = (stateSection as { children?: vscode.TreeItem[] })
				.children;
			expect(stateChildren?.[0]?.description).toBe("1");
		});

		it("ignores setDebugSession after markEnded", async () => {
			const provider = new StateProvider();
			provider.markEnded();
			const session = makeSession(async () => ({ state: { v: 1 } }));
			provider.setDebugSession(session as unknown as vscode.DebugSession);
			const partition = makePartitionElement("p1");
			const items = await provider.getChildren(partition);
			// No live session honoured -> falls through to cache miss.
			expect(items[0]?.label).toMatch(/not loaded/);
		});
	});

	describe("clear", () => {
		it("resets to placeholder", async () => {
			const provider = new StateProvider();
			provider.updateFromState({ state: { v: 1 } });
			vi.advanceTimersByTime(50);
			provider.clear();
			const items = await provider.getChildren();
			expect(items[0]?.label).toBe("No state yet");
		});

		it("flushes the partition cache", async () => {
			const provider = new StateProvider();
			const session = makeSession(async () => ({ state: { v: 1 } }));
			provider.setDebugSession(session as unknown as vscode.DebugSession);
			const partition = makePartitionElement("p1");
			vi.useRealTimers();
			await provider.getChildren(partition); // populate cache
			provider.clear();
			provider.markEnded();
			const items = await provider.getChildren(partition);
			expect(items[0]?.label).toMatch(/not loaded/);
		});
	});
});
