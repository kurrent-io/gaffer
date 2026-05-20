import * as vscode from "vscode";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
	__resetUpdatePromptStateForTests,
	isCliUpdatePromptSuppressed,
	isNewerVersion,
	runNpmUpdate,
	showCliUpdatePrompt,
} from "./update-prompt.js";
import { makeContext } from "../../test/testutil/fake-context.js";
import {
	fireTerminalClosed,
	getState,
	getStatusBarItems,
	queueQuickPick,
	resetVscode,
	setConfiguration,
} from "../../test/testutil/vscode-state.js";
import type { UpdatePromptDeps } from "./update-prompt.js";

const COMMAND_OPEN = "gaffer._cliUpdate.open";

function makeDeps(overrides: Partial<UpdatePromptDeps> = {}): UpdatePromptDeps {
	return {
		context: makeContext(),
		current: "0.1.0",
		latest: "0.2.0",
		runUpdate: vi.fn(),
		onUpdated: vi.fn(),
		...overrides,
	};
}

// Drive the status bar item's click handler. Mirrors what VS Code
// does internally when the user clicks the item.
async function clickStatusBar(): Promise<void> {
	await vscode.commands.executeCommand(COMMAND_OPEN);
}

describe("showCliUpdatePrompt", () => {
	beforeEach(() => {
		resetVscode();
		__resetUpdatePromptStateForTests();
		vi.restoreAllMocks();
	});

	it("creates a single status bar item with the version and tooltip", () => {
		showCliUpdatePrompt(makeDeps({ current: "0.1.0", latest: "0.2.0" }));
		const items = getStatusBarItems();
		expect(items).toHaveLength(1);
		expect(items[0]?.text).toBe("$(arrow-circle-up) gaffer 0.2.0");
		expect(items[0]?.tooltip).toBe(
			"gaffer 0.2.0 is available (you have 0.1.0). Click to update.",
		);
		expect(items[0]?.showCount).toBe(1);
		expect(items[0]?.command).toBe(COMMAND_OPEN);
	});

	it("Update: runs the upgrade, triggers onUpdated on success, and dismisses the item", async () => {
		const runUpdate = vi.fn().mockResolvedValue({ ok: true });
		const onUpdated = vi.fn().mockResolvedValue(undefined);
		showCliUpdatePrompt(makeDeps({ runUpdate, onUpdated }));
		queueQuickPick({ label: "Update" });
		await clickStatusBar();
		expect(runUpdate).toHaveBeenCalledTimes(1);
		expect(onUpdated).toHaveBeenCalledTimes(1);
		expect(getStatusBarItems()[0]?.disposed).toBe(true);
	});

	it("Update failure: leaves the item visible so the user can retry", async () => {
		const ctx = makeContext();
		showCliUpdatePrompt(
			makeDeps({
				context: ctx,
				runUpdate: vi.fn().mockResolvedValue({ ok: false }),
			}),
		);
		queueQuickPick({ label: "Update" });
		await clickStatusBar();
		// Item stays visible; user can re-click to retry.
		expect(getStatusBarItems()[0]?.disposed).toBe(false);
		// A second click reopens the quickpick rather than no-opping.
		queueQuickPick(undefined);
		await clickStatusBar();
		expect(getStatusBarItems()[0]?.disposed).toBe(false);
	});

	it("Update: swallows a runUpdate rejection without crashing", async () => {
		const runUpdate = vi.fn().mockRejectedValue(new Error("spawn failed"));
		const onUpdated = vi.fn();
		showCliUpdatePrompt(makeDeps({ runUpdate, onUpdated }));
		queueQuickPick({ label: "Update" });
		await expect(clickStatusBar()).resolves.toBeUndefined();
		expect(onUpdated).not.toHaveBeenCalled();
	});

	it("Skip this version: records the version on globalState and dismisses the item", async () => {
		const ctx = makeContext();
		showCliUpdatePrompt(makeDeps({ context: ctx, latest: "0.2.0" }));
		queueQuickPick({ label: "Skip this version" });
		await clickStatusBar();
		expect(isCliUpdatePromptSuppressed(ctx, "0.2.0")).toBe(true);
		expect(isCliUpdatePromptSuppressed(ctx, "0.3.0")).toBe(false);
		expect(getStatusBarItems()[0]?.disposed).toBe(true);
	});

	it("Never ask: flips gaffer.cliUpdateNotifications to false and dismisses the item", async () => {
		const ctx = makeContext();
		showCliUpdatePrompt(makeDeps({ context: ctx }));
		queueQuickPick({ label: "Never ask" });
		await clickStatusBar();
		expect(
			vscode.workspace
				.getConfiguration("gaffer")
				.get<boolean>("cliUpdateNotifications"),
		).toBe(false);
		expect(isCliUpdatePromptSuppressed(ctx, "0.2.0")).toBe(true);
		expect(getStatusBarItems()[0]?.disposed).toBe(true);
	});

	it("Quickpick dismissed (Esc): leaves the item visible and no state changed", async () => {
		const ctx = makeContext();
		showCliUpdatePrompt(makeDeps({ context: ctx }));
		queueQuickPick(undefined);
		await clickStatusBar();
		expect(getStatusBarItems()[0]?.disposed).toBe(false);
		expect(isCliUpdatePromptSuppressed(ctx, "0.2.0")).toBe(false);
	});

	it("Swallows a showQuickPick rejection without bubbling", async () => {
		showCliUpdatePrompt(makeDeps());
		vi.spyOn(vscode.window, "showQuickPick").mockRejectedValue(
			new Error("quickpick failed"),
		);
		await expect(clickStatusBar()).resolves.toBeUndefined();
	});

	it("Does not stack a second item while one is already visible", () => {
		showCliUpdatePrompt(makeDeps());
		showCliUpdatePrompt(makeDeps({ latest: "0.2.1" }));
		// Subsequent shows no-op while the first item is visible.
		// The original (0.2.0) remains; a newer version doesn't
		// stack on top.
		expect(getStatusBarItems()).toHaveLength(1);
		expect(getStatusBarItems()[0]?.text).toBe(
			"$(arrow-circle-up) gaffer 0.2.0",
		);
	});
});

describe("isCliUpdatePromptSuppressed", () => {
	beforeEach(() => {
		resetVscode();
		__resetUpdatePromptStateForTests();
	});

	it("returns false on a fresh context", () => {
		expect(isCliUpdatePromptSuppressed(makeContext(), "0.2.0")).toBe(false);
	});

	it("returns true when gaffer.cliUpdateNotifications is false", () => {
		setConfiguration("gaffer", "cliUpdateNotifications", { value: false });
		expect(isCliUpdatePromptSuppressed(makeContext(), "0.2.0")).toBe(true);
	});

	// dismissed=1.2.3, latest=1.2.4: must re-prompt for the newer
	// version. The ticket explicitly calls this out as the reason
	// we need a semver compare instead of string match.
	it("returns false when dismissed is older than latest", async () => {
		const ctx = makeContext();
		await ctx.globalState.update("gaffer.cliUpdate.dismissedVersion", "1.2.3");
		expect(isCliUpdatePromptSuppressed(ctx, "1.2.4")).toBe(false);
	});

	it("returns true when dismissed equals latest", async () => {
		const ctx = makeContext();
		await ctx.globalState.update("gaffer.cliUpdate.dismissedVersion", "1.2.3");
		expect(isCliUpdatePromptSuppressed(ctx, "1.2.3")).toBe(true);
	});

	it("returns true when dismissed is newer than latest", async () => {
		const ctx = makeContext();
		await ctx.globalState.update("gaffer.cliUpdate.dismissedVersion", "1.2.4");
		expect(isCliUpdatePromptSuppressed(ctx, "1.2.3")).toBe(true);
	});
});

// Only the safety wrapper around semver.gt is ours; trust semver for
// actual version comparison.
describe("isNewerVersion", () => {
	it("returns false on garbage input rather than throwing", () => {
		expect(isNewerVersion("not-a-version", "1.2.3")).toBe(false);
		expect(isNewerVersion("1.2.3", "also-not-a-version")).toBe(false);
	});
});

describe("runNpmUpdate", () => {
	beforeEach(() => {
		resetVscode();
		__resetUpdatePromptStateForTests();
	});

	it("spawns a terminal running npm install -g @kurrent/gaffer@latest", async () => {
		const promise = runNpmUpdate();
		const [terminal] = getState().terminals;
		if (!terminal) throw new Error("no terminal spawned");
		expect(terminal.options.name).toBe("KurrentDB Projections: Update CLI");
		expect(terminal.options.shellArgs).toEqual([
			"install",
			"-g",
			"@kurrent/gaffer@latest",
		]);
		expect(terminal.showCount).toBe(1);
		fireTerminalClosed(terminal, 0);
		await expect(promise).resolves.toEqual({ ok: true });
	});

	it("reports ok=false when the terminal exits non-zero", async () => {
		const promise = runNpmUpdate();
		const [terminal] = getState().terminals;
		if (!terminal) throw new Error("no terminal spawned");
		fireTerminalClosed(terminal, 1);
		await expect(promise).resolves.toEqual({ ok: false });
	});
});
