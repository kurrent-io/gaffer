import * as vscode from "vscode";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
	dismissCommandUnresolvedPrompt,
	showCommandUnresolvedPrompt,
} from "./command-unresolved-prompt.js";
import {
	getStatusBarItems,
	queueQuickPick,
	resetVscode,
	setCommandHandler,
} from "../../test/testutil/vscode-state.js";

const COMMAND_OPEN = "gaffer._cliCommand.open";

async function clickStatusBar(): Promise<void> {
	await vscode.commands.executeCommand(COMMAND_OPEN);
}

describe("showCommandUnresolvedPrompt", () => {
	beforeEach(() => {
		resetVscode();
		vi.restoreAllMocks();
	});

	it("creates a red status bar item with the configured value in the tooltip", () => {
		showCommandUnresolvedPrompt({ configured: ["gwaffer"] });
		const items = getStatusBarItems();
		expect(items).toHaveLength(1);
		expect(items[0]?.text).toBe("$(error) gaffer.command unresolved");
		expect(items[0]?.tooltip).toBe(
			'gaffer.command=["gwaffer"] not found. Click to fix.',
		);
		expect(items[0]?.command).toBe(COMMAND_OPEN);
	});

	it("Open settings: executes the openSettings command, item stays visible", async () => {
		const openSettings = vi.fn();
		setCommandHandler("workbench.action.openSettings", openSettings);
		showCommandUnresolvedPrompt({ configured: ["gwaffer"] });
		queueQuickPick({ label: "Open settings" });
		await clickStatusBar();
		expect(openSettings).toHaveBeenCalledTimes(1);
		expect(openSettings).toHaveBeenCalledWith("gaffer.command");
		// Item stays - user might cancel out of settings without
		// fixing the value.
		expect(getStatusBarItems()[0]?.disposed).toBe(false);
	});

	it("Reset to default: clears the user-scope gaffer.command and dismisses the item", async () => {
		// Seed a customised value so we can verify it gets cleared.
		await vscode.workspace
			.getConfiguration("gaffer")
			.update("command", ["gwaffer"], vscode.ConfigurationTarget.Global);
		showCommandUnresolvedPrompt({ configured: ["gwaffer"] });
		queueQuickPick({ label: "Reset to default" });
		await clickStatusBar();
		expect(
			vscode.workspace.getConfiguration("gaffer").get<string[]>("command"),
		).toBeUndefined();
		expect(getStatusBarItems()[0]?.disposed).toBe(true);
	});

	it("Quickpick dismissed (Esc): leaves the item visible and no state changed", async () => {
		showCommandUnresolvedPrompt({ configured: ["gwaffer"] });
		queueQuickPick(undefined);
		await clickStatusBar();
		expect(getStatusBarItems()[0]?.disposed).toBe(false);
	});

	it("Swallows a showQuickPick rejection without bubbling", async () => {
		showCommandUnresolvedPrompt({ configured: ["gwaffer"] });
		vi.spyOn(vscode.window, "showQuickPick").mockRejectedValue(
			new Error("quickpick failed"),
		);
		await expect(clickStatusBar()).resolves.toBeUndefined();
	});

	it("Reuses the existing item with updated tooltip when configured changes", () => {
		// User changes gaffer.command from one typo to another while
		// the prompt is up: the tooltip must reflect the latest
		// configured value, not the original one.
		showCommandUnresolvedPrompt({ configured: ["gwaffer"] });
		showCommandUnresolvedPrompt({ configured: ["different"] });
		expect(getStatusBarItems()).toHaveLength(1);
		expect(getStatusBarItems()[0]?.tooltip).toContain("different");
		expect(getStatusBarItems()[0]?.tooltip).not.toContain("gwaffer");
	});
});

describe("dismissCommandUnresolvedPrompt", () => {
	beforeEach(() => {
		resetVscode();
	});

	it("disposes a visible item", () => {
		showCommandUnresolvedPrompt({ configured: ["gwaffer"] });
		expect(getStatusBarItems()[0]?.disposed).toBe(false);
		dismissCommandUnresolvedPrompt();
		expect(getStatusBarItems()[0]?.disposed).toBe(true);
	});

	it("is a no-op when no item is visible", () => {
		dismissCommandUnresolvedPrompt();
		expect(getStatusBarItems()).toHaveLength(0);
	});
});
