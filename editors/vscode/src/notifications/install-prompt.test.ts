import * as vscode from "vscode";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
	INSTALL_DOCS_URL,
	clearInstallPromptDismissed,
	isInstallPromptDismissed,
	runNpmInstall,
	showCliNotFoundPrompt,
} from "./install-prompt.js";
import { makeContext } from "../../test/testutil/fake-context.js";
import {
	fireTerminalClosed,
	getShownMessages,
	getState,
	queueMessageResponse,
	resetVscode,
} from "../../test/testutil/vscode-state.js";

describe("showCliNotFoundPrompt", () => {
	beforeEach(() => {
		resetVscode();
	});

	it("Install: runs the installer and triggers onInstalled on success", async () => {
		const ctx = makeContext();
		const runInstall = vi.fn().mockResolvedValue({ ok: true });
		const onInstalled = vi.fn().mockResolvedValue(undefined);
		queueMessageResponse("Install");
		await showCliNotFoundPrompt({ context: ctx, runInstall, onInstalled });
		expect(runInstall).toHaveBeenCalledTimes(1);
		expect(onInstalled).toHaveBeenCalledTimes(1);
		expect(isInstallPromptDismissed(ctx)).toBe(false);
	});

	it("Install: does not call onInstalled when the installer reports failure", async () => {
		const ctx = makeContext();
		const runInstall = vi.fn().mockResolvedValue({ ok: false });
		const onInstalled = vi.fn();
		queueMessageResponse("Install");
		await showCliNotFoundPrompt({ context: ctx, runInstall, onInstalled });
		expect(runInstall).toHaveBeenCalledTimes(1);
		expect(onInstalled).not.toHaveBeenCalled();
		// Failure leaves dismissed state alone - the user will be re-prompted
		// next activation.
		expect(isInstallPromptDismissed(ctx)).toBe(false);
	});

	it("Install: swallows a runInstall rejection so the void-fired prompt never throws", async () => {
		const ctx = makeContext();
		const runInstall = vi.fn().mockRejectedValue(new Error("spawn failed"));
		const onInstalled = vi.fn();
		queueMessageResponse("Install");
		// The promise resolves rather than rejects: the prompt handles
		// the throw internally.
		await expect(
			showCliNotFoundPrompt({ context: ctx, runInstall, onInstalled }),
		).resolves.toBeUndefined();
		expect(runInstall).toHaveBeenCalledTimes(1);
		expect(onInstalled).not.toHaveBeenCalled();
	});

	it("Install guide: opens the docs URL via openExternal", async () => {
		const ctx = makeContext();
		const open = vi.spyOn(vscode.env, "openExternal");
		queueMessageResponse("Install guide");
		await showCliNotFoundPrompt({
			context: ctx,
			runInstall: vi.fn(),
			onInstalled: vi.fn(),
		});
		expect(open).toHaveBeenCalledTimes(1);
		const arg = open.mock.calls[0]?.[0] as vscode.Uri;
		// The mock's Uri.parse is loose; assert scheme + the
		// docs-host substring rather than rely on toString round-
		// tripping verbatim.
		expect(arg.scheme).toBe("https");
		expect(arg.path).toContain("docs.kurrent.io/gaffer");
		expect(INSTALL_DOCS_URL).toBe("https://docs.kurrent.io/gaffer/");
		expect(isInstallPromptDismissed(ctx)).toBe(false);
	});

	it("Dismiss: persists the workspace-state flag", async () => {
		const ctx = makeContext();
		queueMessageResponse("Dismiss");
		await showCliNotFoundPrompt({
			context: ctx,
			runInstall: vi.fn(),
			onInstalled: vi.fn(),
		});
		expect(isInstallPromptDismissed(ctx)).toBe(true);
	});

	it("toast dismissed (X / focus loss): leaves the workspace flag alone", async () => {
		const ctx = makeContext();
		// No queued response -> mock returns undefined.
		await showCliNotFoundPrompt({
			context: ctx,
			runInstall: vi.fn(),
			onInstalled: vi.fn(),
		});
		expect(isInstallPromptDismissed(ctx)).toBe(false);
	});

	it("renders the expected toast wording and buttons", async () => {
		const ctx = makeContext();
		queueMessageResponse(undefined);
		await showCliNotFoundPrompt({
			context: ctx,
			runInstall: vi.fn(),
			onInstalled: vi.fn(),
		});
		const messages = getShownMessages();
		expect(messages).toHaveLength(1);
		expect(messages[0]?.kind).toBe("warning");
		expect(messages[0]?.message).toBe(
			"gaffer CLI not found on PATH. Install globally with npm?",
		);
		expect(messages[0]?.items).toEqual(["Install", "Install guide", "Dismiss"]);
	});
});

describe("clearInstallPromptDismissed", () => {
	beforeEach(() => {
		resetVscode();
	});

	it("removes a previously set dismissal flag", async () => {
		const ctx = makeContext();
		queueMessageResponse("Dismiss");
		await showCliNotFoundPrompt({
			context: ctx,
			runInstall: vi.fn(),
			onInstalled: vi.fn(),
		});
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

	it("reports ok=true when the terminal exits with code 0", async () => {
		const promise = runNpmInstall();
		const [terminal] = getState().terminals;
		if (!terminal) throw new Error("no terminal spawned");
		fireTerminalClosed(terminal, 0);
		await expect(promise).resolves.toEqual({ ok: true });
	});

	it("reports ok=false when the terminal exits with non-zero code", async () => {
		const promise = runNpmInstall();
		const [terminal] = getState().terminals;
		if (!terminal) throw new Error("no terminal spawned");
		fireTerminalClosed(terminal, 1);
		await expect(promise).resolves.toEqual({ ok: false });
	});

	it("ignores close events from unrelated terminals", async () => {
		const promise = runNpmInstall();
		const [terminal] = getState().terminals;
		if (!terminal) throw new Error("no terminal spawned");
		const unrelated = vscode.window.createTerminal({ name: "other" });
		fireTerminalClosed(unrelated, 0);
		// Promise must still be pending - if it resolved here, the
		// unrelated terminal's close leaked through.
		let settled = false;
		void promise.then(() => {
			settled = true;
		});
		await Promise.resolve();
		expect(settled).toBe(false);
		fireTerminalClosed(terminal, 0);
		await expect(promise).resolves.toEqual({ ok: true });
	});
});
