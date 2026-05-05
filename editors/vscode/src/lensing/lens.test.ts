import * as vscode from "vscode";
import { describe, expect, it } from "vitest";
import { buildLens, lensState } from "./lens.js";
import { okManifest } from "../../test/testutil/fixtures.js";
import { setTrusted } from "../../test/testutil/vscode-state.js";
import type { Manifest } from "../discovery/schemas.js";
import type { DebugState } from "../types.js";

const range = new vscode.Range(0, 0, 0, 10);
const tomlUri = vscode.Uri.file("/p/gaffer.toml");
const idle: DebugState = { name: null, status: "idle" };

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

describe("lensState", () => {
	it("returns off when the debug state is for a different projection", () => {
		expect(lensState({ name: "other", status: "running" }, "checkout")).toEqual(
			{
				kind: "off",
			},
		);
	});

	it("returns off when no session is active (idle)", () => {
		expect(lensState({ name: null, status: "idle" }, "checkout")).toEqual({
			kind: "off",
		});
	});

	it("returns off when the session has ended (post-mortem)", () => {
		// Name still matches (post-mortem state), but the projection-level
		// lens should fall back to a Debug button. Equivalent for the
		// dropdown: it should re-appear once a session ends.
		expect(
			lensState({ name: "checkout", status: "ended" }, "checkout"),
		).toEqual({ kind: "off" });
	});

	it("returns stop with starting title during attach", () => {
		// Title carries the user-visible string so callers don't have
		// to re-derive it from status.
		const state = lensState(
			{ name: "checkout", status: "starting" },
			"checkout",
		);
		expect(state.kind).toBe("stop");
		if (state.kind === "stop") {
			expect(state.title).toContain("Starting");
		}
	});

	it("returns stop for running and inspecting", () => {
		for (const status of ["running", "inspecting"] as const) {
			const state = lensState({ name: "checkout", status }, "checkout");
			expect(state.kind).toBe("stop");
			if (state.kind === "stop") {
				expect(state.title).toContain("Debugging");
			}
		}
	});
});
