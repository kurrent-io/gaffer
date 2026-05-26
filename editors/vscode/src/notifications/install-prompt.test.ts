import * as vscode from "vscode";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
	__resetInstallPromptStateForTests,
	INSTALL_DOCS_URL,
	clearInstallPromptDismissed,
	isInstallPromptDismissed,
	runNpmInstall,
	showCliNotFoundPrompt,
} from "./install-prompt.js";
import { makeContext } from "../../test/testutil/fake-context.js";
import {
	fireTerminalClosed,
	getState,
	getStatusBarItems,
	queueQuickPick,
	resetVscode,
} from "../../test/testutil/vscode-state.js";
import type { InstallPromptDeps } from "./install-prompt.js";

const COMMAND_OPEN = "gaffer._cliInstall.open";

function makeDeps(
	overrides: Partial<InstallPromptDeps> = {},
): InstallPromptDeps {
	return {
		context: makeContext(),
		runInstall: vi.fn(),
		onInstalled: vi.fn(),
		...overrides,
	};
}

async function clickStatusBar(): Promise<void> {
	await vscode.commands.executeCommand(COMMAND_OPEN);
}

describe("showCliNotFoundPrompt", () => {
	beforeEach(() => {
		resetVscode();
		__resetInstallPromptStateForTests();
		vi.restoreAllMocks();
	});

	it("creates a status bar item with the install prompt text and tooltip", () => {
		showCliNotFoundPrompt(makeDeps());
		const items = getStatusBarItems();
		expect(items).toHaveLength(1);
		expect(items[0]?.text).toBe("$(error) gaffer not installed");
		expect(items[0]?.tooltip).toBe(
			"gaffer CLI not found on PATH. Click to install.",
		);
		expect(items[0]?.command).toBe(COMMAND_OPEN);
	});

	it("does not create an item when the workspace has dismissed the prompt", async () => {
		const ctx = makeContext();
		await ctx.workspaceState.update(
			"gaffer.cliMissingNotificationDismissed",
			true,
		);
		showCliNotFoundPrompt(makeDeps({ context: ctx }));
		expect(getStatusBarItems()).toHaveLength(0);
	});

	it("Install: runs the installer and triggers onInstalled on success", async () => {
		const runInstall = vi.fn().mockResolvedValue({ ok: true });
		const onInstalled = vi.fn().mockResolvedValue(undefined);
		showCliNotFoundPrompt(makeDeps({ runInstall, onInstalled }));
		queueQuickPick({ label: "Install" });
		await clickStatusBar();
		expect(runInstall).toHaveBeenCalledTimes(1);
		expect(onInstalled).toHaveBeenCalledTimes(1);
		expect(getStatusBarItems()[0]?.disposed).toBe(true);
	});

	it("Install failure: leaves the item visible so the user can retry", async () => {
		const runInstall = vi.fn().mockResolvedValue({ ok: false });
		const onInstalled = vi.fn();
		showCliNotFoundPrompt(makeDeps({ runInstall, onInstalled }));
		queueQuickPick({ label: "Install" });
		await clickStatusBar();
		expect(runInstall).toHaveBeenCalledTimes(1);
		expect(onInstalled).not.toHaveBeenCalled();
		expect(getStatusBarItems()[0]?.disposed).toBe(false);
	});

	it("Install: swallows a runInstall rejection without crashing", async () => {
		const runInstall = vi.fn().mockRejectedValue(new Error("spawn failed"));
		const onInstalled = vi.fn();
		showCliNotFoundPrompt(makeDeps({ runInstall, onInstalled }));
		queueQuickPick({ label: "Install" });
		await expect(clickStatusBar()).resolves.toBeUndefined();
		expect(onInstalled).not.toHaveBeenCalled();
	});

	it("Install: swallows an onInstalled rejection without bubbling", async () => {
		const runInstall = vi.fn().mockResolvedValue({ ok: true });
		const onInstalled = vi.fn().mockRejectedValue(new Error("reload failed"));
		showCliNotFoundPrompt(makeDeps({ runInstall, onInstalled }));
		queueQuickPick({ label: "Install" });
		await expect(clickStatusBar()).resolves.toBeUndefined();
		expect(onInstalled).toHaveBeenCalledTimes(1);
	});

	it("Install guide: opens the docs URL and leaves the item visible", async () => {
		const open = vi.spyOn(vscode.env, "openExternal");
		showCliNotFoundPrompt(makeDeps());
		queueQuickPick({ label: "Install guide" });
		await clickStatusBar();
		expect(open).toHaveBeenCalledTimes(1);
		const arg = open.mock.calls[0]?.[0] as vscode.Uri;
		expect(arg.scheme).toBe("https");
		expect(arg.path).toContain("gaffer.kurrent.io/getting-started/install/");
		// Docs is read-and-return; the item stays so the user can
		// click Install afterwards.
		expect(getStatusBarItems()[0]?.disposed).toBe(false);
	});

	it("swallows an openExternal rejection without bubbling", async () => {
		vi.spyOn(vscode.env, "openExternal").mockRejectedValue(
			new Error("no handler"),
		);
		showCliNotFoundPrompt(makeDeps());
		queueQuickPick({ label: "Install guide" });
		await expect(clickStatusBar()).resolves.toBeUndefined();
	});

	it("Dismiss: persists the workspace flag and disposes the item", async () => {
		const ctx = makeContext();
		showCliNotFoundPrompt(makeDeps({ context: ctx }));
		queueQuickPick({ label: "Dismiss" });
		await clickStatusBar();
		expect(isInstallPromptDismissed(ctx)).toBe(true);
		expect(getStatusBarItems()[0]?.disposed).toBe(true);
	});

	it("Quickpick dismissed (Esc): leaves the item visible and no state changed", async () => {
		const ctx = makeContext();
		showCliNotFoundPrompt(makeDeps({ context: ctx }));
		queueQuickPick(undefined);
		await clickStatusBar();
		expect(getStatusBarItems()[0]?.disposed).toBe(false);
		expect(isInstallPromptDismissed(ctx)).toBe(false);
	});

	it("Reuses the existing item across repeated shows", () => {
		showCliNotFoundPrompt(makeDeps());
		showCliNotFoundPrompt(makeDeps());
		expect(getStatusBarItems()).toHaveLength(1);
	});

	it("docs URL points at the gaffer install page", () => {
		expect(INSTALL_DOCS_URL).toBe(
			"https://gaffer.kurrent.io/getting-started/install/",
		);
	});
});

describe("clearInstallPromptDismissed", () => {
	beforeEach(() => {
		resetVscode();
		__resetInstallPromptStateForTests();
	});

	it("removes a previously set dismissal flag", async () => {
		const ctx = makeContext();
		showCliNotFoundPrompt(makeDeps({ context: ctx }));
		queueQuickPick({ label: "Dismiss" });
		await clickStatusBar();
		expect(isInstallPromptDismissed(ctx)).toBe(true);
		await clearInstallPromptDismissed(ctx);
		expect(isInstallPromptDismissed(ctx)).toBe(false);
	});

	it("is a no-op when the flag was never set", async () => {
		const ctx = makeContext();
		await clearInstallPromptDismissed(ctx);
		expect(isInstallPromptDismissed(ctx)).toBe(false);
	});
});

describe("runNpmInstall", () => {
	beforeEach(() => {
		resetVscode();
		__resetInstallPromptStateForTests();
	});

	it("spawns a terminal running npm install -g @kurrent/gaffer", async () => {
		const promise = runNpmInstall();
		const [terminal] = getState().terminals;
		if (!terminal) throw new Error("no terminal spawned");
		expect(terminal.options.name).toBe("KurrentDB Projections: Install CLI");
		expect(terminal.options.shellArgs).toEqual([
			"install",
			"-g",
			"@kurrent/gaffer",
		]);
		expect(terminal.showCount).toBe(1);
		fireTerminalClosed(terminal, 0);
		await expect(promise).resolves.toEqual({ ok: true });
	});

	it("reports ok=false when the terminal exits non-zero", async () => {
		const promise = runNpmInstall();
		const [terminal] = getState().terminals;
		if (!terminal) throw new Error("no terminal spawned");
		fireTerminalClosed(terminal, 1);
		await expect(promise).resolves.toEqual({ ok: false });
	});
});
