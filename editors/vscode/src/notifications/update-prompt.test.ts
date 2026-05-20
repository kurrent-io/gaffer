import * as vscode from "vscode";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
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
} from "../../test/testutil/vscode-state.js";

describe("showCliUpdatePrompt", () => {
	beforeEach(() => {
		resetVscode();
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

	it("Never ask: records the perma-suppress flag in globalState", async () => {
		const ctx = makeContext();
		queueMessageResponse("Never ask");
		await showCliUpdatePrompt({
			context: ctx,
			current: "0.1.0",
			latest: "0.2.0",
			runUpdate: vi.fn(),
			onUpdated: vi.fn(),
		});
		// Suppressed regardless of how new the manifest reports.
		expect(isCliUpdatePromptSuppressed(ctx, "0.2.0")).toBe(true);
		expect(isCliUpdatePromptSuppressed(ctx, "9.9.9")).toBe(true);
	});

	it("toast dismissed (X / focus loss): leaves both globalState flags alone", async () => {
		const ctx = makeContext();
		await showCliUpdatePrompt({
			context: ctx,
			current: "0.1.0",
			latest: "0.2.0",
			runUpdate: vi.fn(),
			onUpdated: vi.fn(),
		});
		expect(isCliUpdatePromptSuppressed(ctx, "0.2.0")).toBe(false);
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
	});

	it("returns false on a fresh context", () => {
		expect(isCliUpdatePromptSuppressed(makeContext(), "0.2.0")).toBe(false);
	});

	it("returns true when never-ask is set", async () => {
		const ctx = makeContext();
		await ctx.globalState.update("gaffer.cliUpdate.neverAsk", true);
		expect(isCliUpdatePromptSuppressed(ctx, "0.2.0")).toBe(true);
	});

	// dismissed=1.2.3, latest=1.2.4: must re-prompt for the newer
	// version. This is the case the ticket explicitly calls out and the
	// reason we need a semver compare instead of string match.
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

	// Defensive: the registry shouldn't ever publish a lower version,
	// but if it did we shouldn't pester the user about a version they
	// already opted out of.
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
