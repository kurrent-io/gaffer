import * as vscode from "vscode";
import { describe, expect, it } from "vitest";
import { buildLens } from "./lens.js";
import { setTrusted } from "../../test/testutil/vscode-state.js";
import type { Manifest } from "../discovery/schemas.js";
import type { DebugState } from "../types.js";

const range = new vscode.Range(0, 0, 0, 10);
const tomlUri = vscode.Uri.file("/p/gaffer.toml");
const idle: DebugState = { name: null, status: "idle" };
const okManifest: Manifest = {
	version: "1.0.0",
	commands: { dev: { flags: ["debug"] } },
};

describe("buildLens", () => {
	it("returns a Debug lens when manifest supports `dev --debug` and idle", () => {
		setTrusted(true);
		const lens = buildLens(okManifest, idle, "checkout", range, tomlUri);
		expect(lens?.command).toEqual({
			title: "$(debug-start) Debug",
			command: "gaffer.debugProjection",
			arguments: [{ name: "checkout", tomlUri }],
		});
	});

	it("returns a Trust lens when workspace is untrusted, regardless of manifest", () => {
		setTrusted(false);
		const lens = buildLens(okManifest, idle, "checkout", range, tomlUri);
		expect(lens?.command?.command).toBe("workbench.trust.manage");
	});

	it("returns null when the manifest is missing", () => {
		setTrusted(true);
		expect(buildLens(null, idle, "checkout", range, tomlUri)).toBeNull();
	});

	it("returns null when manifest has no `dev` command", () => {
		setTrusted(true);
		const m: Manifest = { version: "1.0.0", commands: {} };
		expect(buildLens(m, idle, "checkout", range, tomlUri)).toBeNull();
	});

	it("returns null when `dev` lacks the `debug` flag", () => {
		setTrusted(true);
		const m: Manifest = {
			version: "1.0.0",
			commands: { dev: { flags: [] } },
		};
		expect(buildLens(m, idle, "checkout", range, tomlUri)).toBeNull();
	});

	describe("when this projection is the active session", () => {
		it("renders a Starting (cancel) lens during starting", () => {
			setTrusted(true);
			const state: DebugState = { name: "checkout", status: "starting" };
			const lens = buildLens(okManifest, state, "checkout", range, tomlUri);
			expect(lens?.command).toEqual({
				title: "$(sync~spin) Starting (cancel)",
				command: "gaffer.stopDebug",
			});
		});

		it("renders a Debugging lens during running", () => {
			setTrusted(true);
			const state: DebugState = { name: "checkout", status: "running" };
			const lens = buildLens(okManifest, state, "checkout", range, tomlUri);
			expect(lens?.command).toEqual({
				title: "$(debug-stop) Debugging",
				command: "gaffer.stopDebug",
			});
		});

		it("renders a Debugging lens during inspecting", () => {
			setTrusted(true);
			const state: DebugState = { name: "checkout", status: "inspecting" };
			const lens = buildLens(okManifest, state, "checkout", range, tomlUri);
			expect(lens?.command?.title).toBe("$(debug-stop) Debugging");
		});

		it("falls through to the trust/debug branches when ended", () => {
			setTrusted(true);
			const state: DebugState = { name: "checkout", status: "ended" };
			const lens = buildLens(okManifest, state, "checkout", range, tomlUri);
			expect(lens?.command?.command).toBe("gaffer.debugProjection");
		});
	});

	it("does not match a different projection's debug state", () => {
		setTrusted(true);
		const state: DebugState = { name: "other", status: "running" };
		const lens = buildLens(okManifest, state, "checkout", range, tomlUri);
		expect(lens?.command?.command).toBe("gaffer.debugProjection");
	});
});
