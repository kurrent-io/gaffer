import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { registerTypeScriptPlugin } from "./typescript-plugin.js";
import { makeContext } from "../../test/testutil/fake-context.js";
import {
	fireConfigurationChange,
	resetVscode,
	setConfiguration,
	setExtension,
} from "../../test/testutil/vscode-state.js";
import { flushAllMicrotasks } from "../../test/testutil/promise.js";

const TS_EXT_ID = "vscode.typescript-language-features";

function stubTypeScriptExtension(): {
	configurePlugin: ReturnType<typeof vi.fn>;
} {
	const configurePlugin = vi.fn();
	setExtension(TS_EXT_ID, { getAPI: () => ({ configurePlugin }) });
	return { configurePlugin };
}

describe("registerTypeScriptPlugin", () => {
	beforeEach(() => {
		resetVscode();
	});
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it("pushes the initial config to configurePlugin once", async () => {
		const { configurePlugin } = stubTypeScriptExtension();
		const ctx = makeContext();
		registerTypeScriptPlugin(ctx);
		await flushAllMicrotasks();
		expect(configurePlugin).toHaveBeenCalledTimes(1);
		expect(configurePlugin).toHaveBeenCalledWith("gaffer-tsserver-plugin", {
			typesEntryPath: "/fake/extension/dist/types/projections.d.ts",
			enabled: true,
		});
	});

	it("propagates gaffer.injectProjectionTypes=false as enabled:false", async () => {
		const { configurePlugin } = stubTypeScriptExtension();
		setConfiguration("gaffer", "injectProjectionTypes", { value: false });
		registerTypeScriptPlugin(makeContext());
		await flushAllMicrotasks();
		expect(configurePlugin).toHaveBeenCalledWith(
			"gaffer-tsserver-plugin",
			expect.objectContaining({ enabled: false }),
		);
	});

	it("re-pushes when injectProjectionTypes changes", async () => {
		const { configurePlugin } = stubTypeScriptExtension();
		registerTypeScriptPlugin(makeContext());
		await flushAllMicrotasks();
		expect(configurePlugin).toHaveBeenCalledTimes(1);

		setConfiguration("gaffer", "injectProjectionTypes", { value: false });
		fireConfigurationChange(["gaffer.injectProjectionTypes"]);
		await flushAllMicrotasks();

		expect(configurePlugin).toHaveBeenCalledTimes(2);
		expect(configurePlugin).toHaveBeenLastCalledWith(
			"gaffer-tsserver-plugin",
			expect.objectContaining({ enabled: false }),
		);
	});

	it("ignores unrelated configuration changes", async () => {
		const { configurePlugin } = stubTypeScriptExtension();
		registerTypeScriptPlugin(makeContext());
		await flushAllMicrotasks();
		fireConfigurationChange(["editor.fontSize"]);
		await flushAllMicrotasks();
		expect(configurePlugin).toHaveBeenCalledTimes(1);
	});

	it("dedupes identical re-fires", async () => {
		// configurePlugin triggers a tsserver project reload; pushing the
		// same config a second time would invalidate hover/completions for
		// no benefit.
		const { configurePlugin } = stubTypeScriptExtension();
		registerTypeScriptPlugin(makeContext());
		await flushAllMicrotasks();
		fireConfigurationChange(["gaffer.injectProjectionTypes"]);
		await flushAllMicrotasks();
		expect(configurePlugin).toHaveBeenCalledTimes(1);
	});

	it("registers a configuration-change disposable on the context", () => {
		stubTypeScriptExtension();
		const ctx = makeContext();
		registerTypeScriptPlugin(ctx);
		expect(ctx.subscriptions.length).toBeGreaterThan(0);
	});

	it("no-ops without throwing when typescript-language-features is missing", async () => {
		const ctx = makeContext();
		registerTypeScriptPlugin(ctx);
		await flushAllMicrotasks();
		// Subscription still registered so a future setting toggle can
		// retry once the extension shows up.
		expect(ctx.subscriptions.length).toBeGreaterThan(0);
	});

	it("no-ops when getAPI returns undefined", async () => {
		const configurePlugin = vi.fn();
		setExtension(TS_EXT_ID, { getAPI: () => undefined });
		registerTypeScriptPlugin(makeContext());
		await flushAllMicrotasks();
		expect(configurePlugin).not.toHaveBeenCalled();
	});
});
