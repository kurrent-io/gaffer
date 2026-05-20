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
	getShownMessages,
	getState,
	queueMessageResponse,
	resetVscode,
	setConfiguration,
} from "../../test/testutil/vscode-state.js";

describe("showCliUpdatePrompt", () => {
	beforeEach(() => {
		resetVscode();
		__resetUpdatePromptStateForTests();
		vi.restoreAllMocks();
	});

	it("Update: runs the upgrade and triggers onUpdated on success", async () => {
		const ctx = makeContext();
		const runUpdate = vi.fn().mockResolvedValue({ ok: true });
		const onUpdated = vi.fn().mockResolvedValue(undefined);
		queueMessageResponse("Update");
		await showCliUpdatePrompt({
			context: ctx,
			current: "0.1.0",
			latest: "0.2.0",
			runUpdate,
			onUpdated,
		});
		expect(runUpdate).toHaveBeenCalledTimes(1);
		expect(onUpdated).toHaveBeenCalledTimes(1);
	});

	it("Update: does not call onUpdated when the upgrade reports failure", async () => {
		const ctx = makeContext();
		const runUpdate = vi.fn().mockResolvedValue({ ok: false });
		const onUpdated = vi.fn();
		queueMessageResponse("Update");
		await showCliUpdatePrompt({
			context: ctx,
			current: "0.1.0",
			latest: "0.2.0",
			runUpdate,
			onUpdated,
		});
		expect(runUpdate).toHaveBeenCalledTimes(1);
		expect(onUpdated).not.toHaveBeenCalled();
	});

	// After a failed Update click the user needs an in-session retry
	// path. The lastPromptedVersion guard normally suppresses the
	// next manifest reload's toast for the same version, but a
	// failed action should leave the door open.
	it("Update failure: clears the session guard so the same version re-prompts", async () => {
		const ctx = makeContext();
		queueMessageResponse("Update");
		await showCliUpdatePrompt({
			context: ctx,
			current: "0.1.0",
			latest: "0.2.0",
			runUpdate: vi.fn().mockResolvedValue({ ok: false }),
			onUpdated: vi.fn(),
		});
		queueMessageResponse(undefined);
		await showCliUpdatePrompt({
			context: ctx,
			current: "0.1.0",
			latest: "0.2.0",
			runUpdate: vi.fn(),
			onUpdated: vi.fn(),
		});
		expect(getShownMessages()).toHaveLength(2);
	});

	it("Update: swallows a runUpdate rejection so the void-fired prompt never throws", async () => {
		const ctx = makeContext();
		const runUpdate = vi.fn().mockRejectedValue(new Error("spawn failed"));
		const onUpdated = vi.fn();
		queueMessageResponse("Update");
		await expect(
			showCliUpdatePrompt({
				context: ctx,
				current: "0.1.0",
				latest: "0.2.0",
				runUpdate,
				onUpdated,
			}),
		).resolves.toBeUndefined();
		expect(onUpdated).not.toHaveBeenCalled();
	});

	it("swallows a showInformationMessage rejection without bubbling", async () => {
		const ctx = makeContext();
		vi.spyOn(vscode.window, "showInformationMessage").mockRejectedValue(
			new Error("toast failed"),
		);
		await expect(
			showCliUpdatePrompt({
				context: ctx,
				current: "0.1.0",
				latest: "0.2.0",
				runUpdate: vi.fn(),
				onUpdated: vi.fn(),
			}),
		).resolves.toBeUndefined();
	});

	it("Skip this version: records the latest version in globalState", async () => {
		const ctx = makeContext();
		queueMessageResponse("Skip this version");
		await showCliUpdatePrompt({
			context: ctx,
			current: "0.1.0",
			latest: "0.2.0",
			runUpdate: vi.fn(),
			onUpdated: vi.fn(),
		});
		expect(isCliUpdatePromptSuppressed(ctx, "0.2.0")).toBe(true);
		// A newer version should re-prompt.
		expect(isCliUpdatePromptSuppressed(ctx, "0.3.0")).toBe(false);
	});

	it("Never ask: flips the gaffer.cliUpdateNotifications setting to false", async () => {
		const ctx = makeContext();
		queueMessageResponse("Never ask");
		await showCliUpdatePrompt({
			context: ctx,
			current: "0.1.0",
			latest: "0.2.0",
			runUpdate: vi.fn(),
			onUpdated: vi.fn(),
		});
		expect(
			vscode.workspace
				.getConfiguration("gaffer")
				.get<boolean>("cliUpdateNotifications"),
		).toBe(false);
		// Suppression follows the setting, not a hidden flag.
		expect(isCliUpdatePromptSuppressed(ctx, "0.2.0")).toBe(true);
		expect(isCliUpdatePromptSuppressed(ctx, "9.9.9")).toBe(true);
	});

	it("toast dismissed (X / focus loss): leaves the dismissed-version key alone", async () => {
		const ctx = makeContext();
		await showCliUpdatePrompt({
			context: ctx,
			current: "0.1.0",
			latest: "0.2.0",
			runUpdate: vi.fn(),
			onUpdated: vi.fn(),
		});
		// No suppression flag persisted - a fresh context would still
		// prompt for the same version. The in-session dedupe is via
		// the module-level lastPromptedVersion guard, not state.
		expect(isCliUpdatePromptSuppressed(makeContext(), "0.2.0")).toBe(false);
	});

	it("does not re-prompt within the same session after a dismissed toast", async () => {
		const ctx = makeContext();
		queueMessageResponse(undefined);
		await showCliUpdatePrompt({
			context: ctx,
			current: "0.1.0",
			latest: "0.2.0",
			runUpdate: vi.fn(),
			onUpdated: vi.fn(),
		});
		// Second call with the same latest: the lastPromptedVersion
		// guard suppresses the toast even though no persistent flag
		// was set. A different latest version DOES re-prompt.
		await showCliUpdatePrompt({
			context: ctx,
			current: "0.1.0",
			latest: "0.2.0",
			runUpdate: vi.fn(),
			onUpdated: vi.fn(),
		});
		expect(getShownMessages()).toHaveLength(1);

		queueMessageResponse(undefined);
		await showCliUpdatePrompt({
			context: ctx,
			current: "0.1.0",
			latest: "0.2.1",
			runUpdate: vi.fn(),
			onUpdated: vi.fn(),
		});
		expect(getShownMessages()).toHaveLength(2);
	});

	it("dedupes concurrent calls onto the in-flight prompt", async () => {
		const ctx = makeContext();
		queueMessageResponse("Never ask");
		const first = showCliUpdatePrompt({
			context: ctx,
			current: "0.1.0",
			latest: "0.2.0",
			runUpdate: vi.fn(),
			onUpdated: vi.fn(),
		});
		const second = showCliUpdatePrompt({
			context: ctx,
			current: "0.1.0",
			latest: "0.2.0",
			runUpdate: vi.fn(),
			onUpdated: vi.fn(),
		});
		await Promise.all([first, second]);
		expect(getShownMessages()).toHaveLength(1);
	});

	it("renders the expected toast wording and buttons", async () => {
		const ctx = makeContext();
		queueMessageResponse(undefined);
		await showCliUpdatePrompt({
			context: ctx,
			current: "0.1.0",
			latest: "0.2.0",
			runUpdate: vi.fn(),
			onUpdated: vi.fn(),
		});
		const messages = getShownMessages();
		expect(messages).toHaveLength(1);
		expect(messages[0]?.kind).toBe("info");
		expect(messages[0]?.message).toBe(
			"gaffer 0.2.0 is available (you have 0.1.0). Update?",
		);
		expect(messages[0]?.items).toEqual([
			"Update",
			"Skip this version",
			"Never ask",
		]);
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
