// Shared CodeLens decision tree used by both the TOML and JS providers.
//
// Returns the right lens for the projection's current state:
// - currently being debugged here -> Stop button (debugging or starting)
// - workspace untrusted -> "Trust workspace" prompt
// - manifest not loaded / `dev --debug` not in manifest -> no lens
// - otherwise -> Debug button

import * as vscode from "vscode";
import { hasCommand, hasFlag } from "../discovery/cli.js";
import type { Manifest } from "../discovery/schemas.js";
import type { DebugState } from "../types.js";

// A projection's debug state from a lens's perspective. "stop" means
// the projection-level lens should swap to a Stop button (with the
// embedded title for the current substate); the dropdown should hide.
// "off" means no active session for this projection - render the
// regular Debug affordance.
export type LensState = { kind: "stop"; title: string } | { kind: "off" };

export function lensState(
	debugState: Readonly<DebugState>,
	name: string,
): LensState {
	if (debugState.name !== name) return { kind: "off" };
	switch (debugState.status) {
		case "starting":
			return { kind: "stop", title: "$(sync~spin) Starting (cancel)" };
		case "running":
		case "inspecting":
			return { kind: "stop", title: "$(debug-stop) Debugging" };
		case "idle":
		case "ended":
			return { kind: "off" };
	}
}

export function buildLens(
	manifest: Manifest | null,
	debugState: Readonly<DebugState>,
	name: string,
	range: vscode.Range,
	tomlUri: vscode.Uri,
	fixture?: string,
): vscode.CodeLens | null {
	const state = lensState(debugState, name);
	// Per-fixture lenses (fixture set) stay clickable mid-session so
	// the user can switch to a different fixture; the projection-level
	// lens (fixture undefined) is the one that swaps to Stop.
	if (state.kind === "stop" && fixture === undefined) {
		return new vscode.CodeLens(range, {
			title: state.title,
			command: "gaffer.stopDebug",
		});
	}

	if (!vscode.workspace.isTrusted) {
		return new vscode.CodeLens(range, {
			title: "$(workspace-untrusted) Trust workspace to debug",
			command: "workbench.trust.manage",
		});
	}

	if (!hasCommand(manifest, "dev") || !hasFlag(manifest, "dev", "debug")) {
		return null;
	}

	const args: { name: string; tomlUri: vscode.Uri; fixture?: string } = {
		name,
		tomlUri,
	};
	if (fixture !== undefined) args.fixture = fixture;
	return new vscode.CodeLens(range, {
		title: "$(debug-start) Debug",
		command: "gaffer.debugProjection",
		arguments: [args],
	});
}

// Non-actionable lens for an invalid fixture entry. Title is the
// entire UX; the user fixes the bad fixture in the toml. VS Code
// requires a command, so the click routes to gaffer.noop.
export function buildInvalidFixtureLens(
	range: vscode.Range,
	reason: string,
): vscode.CodeLens {
	return new vscode.CodeLens(range, {
		title: `$(warning) Invalid fixture: ${reason}`,
		command: "gaffer.noop",
	});
}

// "Debug from fixture..." dropdown lens. Shared between the JS and
// TOML providers - identical UX and gating in both contexts. Hidden
// while a session for this projection is mid-flight; the user should
// stop it (or use the per-fixture toml lenses) before launching another.
// Same trust + manifest gating as buildLens.
export function buildPickLens(
	manifest: Manifest | null,
	debugState: Readonly<DebugState>,
	name: string,
	range: vscode.Range,
	tomlUri: vscode.Uri,
	fixtureNames: string[],
): vscode.CodeLens | null {
	if (lensState(debugState, name).kind === "stop") return null;
	if (!vscode.workspace.isTrusted) return null;
	if (!hasCommand(manifest, "dev") || !hasFlag(manifest, "dev", "debug")) {
		return null;
	}
	return new vscode.CodeLens(range, {
		title: "$(debug-start) Debug from fixture...",
		command: "gaffer.debugProjectionPick",
		arguments: [{ name, tomlUri, fixtureNames }],
	});
}
