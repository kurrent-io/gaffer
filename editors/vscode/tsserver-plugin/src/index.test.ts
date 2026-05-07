import { describe, expect, it, vi } from "vitest";
import type ts from "typescript/lib/tsserverlibrary.js";
import init from "./index.js";
import type { PluginConfig } from "./config.js";

interface FakeHost {
	getScriptFileNames: () => string[];
}

interface FakeProject {
	projectService: { logger: { info: (msg: string) => void } };
}

function makeInfo(
	config: PluginConfig,
	base: string[],
): ts.server.PluginCreateInfo {
	const host: FakeHost = { getScriptFileNames: () => [...base] };
	const project: FakeProject = {
		projectService: { logger: { info: vi.fn() } },
	};
	return {
		config,
		languageServiceHost: host,
		project,
		languageService: {} as ts.LanguageService,
	} as unknown as ts.server.PluginCreateInfo;
}

const TYPES = "/abs/types/projections.d.ts";

describe("gaffer-tsserver-plugin", () => {
	it("appends typesEntryPath when enabled", () => {
		const plugin = init({ typescript: {} as typeof ts });
		const info = makeInfo({ typesEntryPath: TYPES, enabled: true }, [
			"/ws/a.js",
		]);
		plugin.create(info);
		expect(info.languageServiceHost.getScriptFileNames()).toEqual([
			"/ws/a.js",
			TYPES,
		]);
	});

	it("returns the program unchanged when enabled is false", () => {
		const plugin = init({ typescript: {} as typeof ts });
		const info = makeInfo({ typesEntryPath: TYPES, enabled: false }, [
			"/ws/a.js",
		]);
		plugin.create(info);
		expect(info.languageServiceHost.getScriptFileNames()).toEqual(["/ws/a.js"]);
	});

	it("returns the program unchanged when typesEntryPath is missing", () => {
		const plugin = init({ typescript: {} as typeof ts });
		const info = makeInfo({ enabled: true }, ["/ws/a.js"]);
		plugin.create(info);
		expect(info.languageServiceHost.getScriptFileNames()).toEqual(["/ws/a.js"]);
	});

	it("does not double-add typesEntryPath if already present", () => {
		const plugin = init({ typescript: {} as typeof ts });
		const info = makeInfo({ typesEntryPath: TYPES, enabled: true }, [
			"/ws/a.js",
			TYPES,
		]);
		plugin.create(info);
		const names = info.languageServiceHost.getScriptFileNames();
		expect(names.filter((n) => n === TYPES)).toHaveLength(1);
	});

	it("only wraps the host once when create is called twice", () => {
		// configurePlugin can re-trigger create() on the same host. Without
		// the WRAPPED marker the second wrap would call into the first
		// wrap, doubling the appended types entry.
		const plugin = init({ typescript: {} as typeof ts });
		const info = makeInfo({ typesEntryPath: TYPES, enabled: true }, [
			"/ws/a.js",
		]);
		plugin.create(info);
		const wrappedOnce = info.languageServiceHost.getScriptFileNames;
		plugin.create(info);
		expect(info.languageServiceHost.getScriptFileNames).toBe(wrappedOnce);
		expect(info.languageServiceHost.getScriptFileNames()).toEqual([
			"/ws/a.js",
			TYPES,
		]);
	});

	it("picks up onConfigurationChanged updates without re-wrapping", () => {
		const plugin = init({ typescript: {} as typeof ts });
		const info = makeInfo({ enabled: true }, ["/ws/a.js"]);
		plugin.create(info);
		expect(info.languageServiceHost.getScriptFileNames()).toEqual(["/ws/a.js"]);
		plugin.onConfigurationChanged?.({ typesEntryPath: TYPES, enabled: true });
		expect(info.languageServiceHost.getScriptFileNames()).toEqual([
			"/ws/a.js",
			TYPES,
		]);
	});
});
