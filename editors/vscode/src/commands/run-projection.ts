// gaffer.runProjection: command-palette flow that picks a projection
// from the LSP-sourced index, then a run mode (live vs fixture), then
// drives the SessionController. Lives outside extension.ts so the
// branching UX logic can be unit-tested in isolation - activate()'s
// job is wire-up, not interactive flows.

import * as vscode from "vscode";
import {
	hasCommand,
	hasFlag,
	tryFetchManifest,
	type SpawnTelemetry,
} from "../discovery/cli.js";
import {
	fetchProjectionDetails,
	fetchProjections,
	type ProjectionDetails,
} from "../lsp/symbols.js";
import { showManifestFailure } from "../notifications/cli.js";
import { showDebugUnsupported } from "../notifications/debug.js";
import { showLspError, showLspNotReady } from "../notifications/lsp.js";
import { showTrustWarning } from "../notifications/trust.js";
import { showNoProjections } from "../notifications/workspace.js";
import type { DebugProjectionArgs } from "../debugging/session-controller.js";
import { buildSourceItems } from "./debug-projection-pick.js";

export interface RunProjectionDeps {
	// SessionController.start, passed by reference rather than the
	// whole controller so this command stays narrow (we only need to
	// kick a session, not stop / inspect / etc.).
	start: (args: DebugProjectionArgs) => Promise<void>;
	// First workspace folder's fsPath for the manifest fetch's cwd.
	// Returns undefined for single-buffer sessions.
	workspaceCwd: () => string | undefined;
	// Spawn-time identity + opt-out signal for the manifest fetch.
	telemetry: SpawnTelemetry;
}

export function runProjection(deps: RunProjectionDeps): () => Promise<void> {
	return async () => {
		if (!vscode.workspace.isTrusted) {
			void showTrustWarning();
			return;
		}
		const result = await fetchProjections();
		if (result.kind === "not-ready") {
			void showLspNotReady();
			return;
		}
		if (result.kind === "error") {
			void showLspError();
			return;
		}
		if (result.projections.length === 0) {
			void showNoProjections();
			return;
		}
		const picked = await vscode.window.showQuickPick(
			result.projections.map((p) => ({
				label: p.name,
				description: vscode.workspace.asRelativePath(p.tomlUri),
				projection: p,
			})),
			{ placeHolder: "Select a projection to debug" },
		);
		if (!picked) return;
		const details = await fetchProjectionDetails(
			picked.projection.name,
			picked.projection.tomlUri,
		);
		const source = await pickRunMode(picked.projection.name, details);
		if (source === undefined) return;
		const manifest = await tryFetchManifest(
			deps.workspaceCwd(),
			deps.telemetry,
			showManifestFailure,
		);
		if (!manifest) return;
		// Same capability gate the lens provider applies. If the
		// installed gaffer can't run `dev --debug`, the lens path
		// hides the lens entirely; the command palette has no lens
		// to hide, so surface the same fact as a toast instead of
		// kicking a session that would fail at attach time.
		if (!hasCommand(manifest, "dev") || !hasFlag(manifest, "dev", "debug")) {
			void showDebugUnsupported();
			return;
		}
		await deps.start({
			name: picked.projection.name,
			tomlUri: picked.projection.tomlUri,
			...source,
		});
	};
}

// The source a run was resolved to: a named fixture, a live env, or
// neither (empty - a live run with no explicit env, letting the CLI
// resolve the default or surface an error). Spread straight into
// DebugProjectionArgs.
export type RunSource = { fixture?: string; env?: string };

// pickRunMode resolves the second-step source pick. Exported for unit
// testing; the runProjection caller is the only production caller.
//
// Returns the chosen RunSource, or undefined if the user dismisses the
// picker. It mirrors the CodeLens "Debug from..." picker
// (buildSourceItems): one row per fixture, then one per configured
// environment (default tagged). A sole source is used without
// prompting. With nothing to choose - no fixtures, no envs, or the LSP
// didn't answer - it falls through to an empty (live default) source so
// the CLI surfaces any resulting error rather than us blocking the run.
export async function pickRunMode(
	projectionName: string,
	details: ProjectionDetails | null,
): Promise<RunSource | undefined> {
	const items = details
		? buildSourceItems(details.fixtures, details.environments)
		: [];
	const [first] = items;
	if (!first) return {};
	if (items.length === 1) return sourceOf(first);
	const picked = await vscode.window.showQuickPick(items, {
		placeHolder: `Run ${projectionName} against...`,
	});
	if (!picked) return undefined;
	return sourceOf(picked);
}

function sourceOf(item: { fixture?: string; env?: string }): RunSource {
	if (item.env !== undefined) return { env: item.env };
	if (item.fixture !== undefined) return { fixture: item.fixture };
	return {};
}
