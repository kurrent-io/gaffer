import * as vscode from "vscode";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { StepProvider } from "./step.js";

describe("StepProvider", () => {
	beforeEach(() => {
		vi.useFakeTimers();
	});
	afterEach(() => {
		vi.useRealTimers();
	});

	it("shows a placeholder when no step is in flight", async () => {
		const provider = new StepProvider();
		const items = provider.getChildren();
		expect(items).toHaveLength(1);
		expect(items[0]?.label).toMatch(/Press Continue/);
	});

	it("startStep replaces previous items with the input event", async () => {
		const provider = new StepProvider();
		provider.addLog("from previous step");
		// Skip the debounce so the previous-step log is in the items
		// before startStep wipes it.
		vi.advanceTimersByTime(50);
		provider.startStep({
			sequenceNumber: 7,
			streamId: "orders-1",
			eventType: "OrderPlaced",
		});
		const items = provider.getChildren();
		expect(items).toHaveLength(1);
		// Plan rule: assert structural invariant, not snapshot.
		expect(items[0]?.label).toBe("7@orders-1");
		expect(items[0]?.description).toBe("OrderPlaced");
	});

	it("addLog accumulates lines under the current step (debounced)", async () => {
		const provider = new StepProvider();
		provider.startStep({
			sequenceNumber: 1,
			streamId: "s",
			eventType: "T",
		});
		provider.addLog("a");
		provider.addLog("b");
		// Inside the 50ms window: subsequent fires schedule once.
		vi.advanceTimersByTime(50);
		const items = provider.getChildren();
		// Input + 2 log items.
		expect(items).toHaveLength(3);
		expect(items[1]?.label).toBe("a");
		expect(items[2]?.label).toBe("b");
	});

	it("addEmit attaches data/metadata as collapsible children", async () => {
		const provider = new StepProvider();
		provider.startStep({
			sequenceNumber: 1,
			streamId: "s",
			eventType: "T",
		});
		provider.addEmit({
			streamId: "out-1",
			eventType: "Emitted",
			data: { x: 1 },
		});
		vi.advanceTimersByTime(50);
		const items = provider.getChildren();
		const emit = items.at(-1);
		expect(emit?.label).toBe("out-1");
		expect(emit?.description).toBe("Emitted");
		expect(emit?.collapsibleState).toBe(
			vscode.TreeItemCollapsibleState.Collapsed,
		);
	});

	it("addEmit collapses to None when there are no children", async () => {
		const provider = new StepProvider();
		provider.startStep({
			sequenceNumber: 1,
			streamId: "s",
			eventType: "T",
		});
		provider.addEmit({ streamId: "out", isLink: true });
		vi.advanceTimersByTime(50);
		const items = provider.getChildren();
		const emit = items.at(-1);
		expect(emit?.collapsibleState).toBe(vscode.TreeItemCollapsibleState.None);
		expect(emit?.description).toBe("link");
	});

	it("setResult appends a processed result", async () => {
		const provider = new StepProvider();
		provider.startStep({
			sequenceNumber: 1,
			streamId: "s",
			eventType: "T",
		});
		provider.setResult({
			status: "processed",
			partition: "p1",
			state: { count: 2 },
		});
		vi.advanceTimersByTime(50);
		const items = provider.getChildren();
		const result = items.at(-1);
		expect(result?.label).toBe("processed");
		expect(result?.description).toBe("[p1]");
		expect(result?.collapsibleState).toBe(
			vscode.TreeItemCollapsibleState.Collapsed,
		);
	});

	it("setResult appends a skipped result", async () => {
		// In live mode (the only mode the extension currently supports)
		// the CLI suppresses skipped events wire-side, so this code
		// path is only exercised by future fixture-mode work (UI-1540).
		// Verifying the mechanical contract here: a skipped result is
		// appended like any other, with its reason as the description.
		const provider = new StepProvider();
		provider.startStep({
			sequenceNumber: 1,
			streamId: "s",
			eventType: "T",
		});
		provider.setResult({ status: "skipped", reason: "filtered out" });
		vi.advanceTimersByTime(50);
		const items = provider.getChildren();
		const result = items.at(-1);
		expect(result?.label).toBe("skipped");
		expect(result?.description).toBe("filtered out");
	});

	it("setError appends an error item with description", async () => {
		const provider = new StepProvider();
		provider.startStep({
			sequenceNumber: 1,
			streamId: "s",
			eventType: "T",
		});
		provider.setError("E_FOO", "boom");
		// setError fires synchronously (not debounced).
		const items = provider.getChildren();
		const err = items.at(-1);
		expect(err?.label).toBe("E_FOO");
		expect(err?.description).toBe("boom");
	});

	it("clear empties the items and the placeholder reappears", async () => {
		const provider = new StepProvider();
		provider.startStep({
			sequenceNumber: 1,
			streamId: "s",
			eventType: "T",
		});
		provider.clear();
		const items = provider.getChildren();
		expect(items).toHaveLength(1);
		expect(items[0]?.label).toMatch(/Press Continue/);
	});

	it("addLog debounces onDidChangeTreeData fires within 50ms", () => {
		const provider = new StepProvider();
		let fires = 0;
		provider.onDidChangeTreeData(() => {
			fires++;
		});
		provider.startStep({
			sequenceNumber: 1,
			streamId: "s",
			eventType: "T",
		});
		// startStep fires synchronously.
		expect(fires).toBe(1);
		provider.addLog("a");
		provider.addLog("b");
		provider.addLog("c");
		// Within the debounce window, no extra fires.
		expect(fires).toBe(1);
		vi.advanceTimersByTime(50);
		expect(fires).toBe(2);
	});
});
