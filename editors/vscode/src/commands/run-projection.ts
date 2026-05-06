// gaffer.runProjection: command-palette flow that picks a projection
// from the LSP-sourced index, then a run mode (live vs fixture), then
// drives the SessionController. Lives outside extension.ts so the
// branching UX logic can be unit-tested in isolation - activate()'s
// job is wire-up, not interactive flows.

import * as vscode from "vscode";
import { hasCommand, hasFlag, tryFetchManifest } from "../discovery/cli.js";
import {
	fetchProjectionDetails,
	fetchProjections,
	type ProjectionDetails,
} from "../lsp/symbols.js";
import {
	showDebugUnsupported,
	showLspError,
	showLspNotReady,
	showManifestFailure,
	showNoProjections,
	showTrustWarning,
} from "../notifications.js";
import type { DebugProjectionArgs } from "../debugging/session-controller.js";

export interface RunProjectionDeps {
	// SessionController.start, passed by reference rather than the
	// whole controller so this command stays narrow (we only need to
	// kick a session, not stop / inspect / etc.).
	start: (args: DebugProjectionArgs) => Promise<void>;
	// First workspace folder's fsPath for the manifest fetch's cwd.
	// Returns undefined for single-buffer sessions.
	workspaceCwd: () => string | undefined;
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
		const fixture = await pickRunMode(picked.projection.name, details);
		if (fixture === undefined) return;
		const manifest = await tryFetchManifest(
			deps.workspaceCwd(),
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
			...(fixture === null ? {} : { fixture }),
		});
	};
}

// pickRunMode resolves the second-step QuickPick. Exported for unit
// testing; the runProjection caller is the only production caller.
//
// Return value:
//   - `undefined`: user dismissed the second pick, abort the run
//   - `null`: live run (no fixture)
//   - `string`: named fixture to run against
//
// When details is null (LSP didn't answer or rejected), default to
// live - the same single-step flow we had before the picker landed.
// When the projection has neither a connection nor any fixtures, also
// default to live; the CLI will surface the resulting error itself
// rather than us blocking the run with a confirmation toast.
export async function pickRunMode(
	projectionName: string,
	details: ProjectionDetails | null,
): Promise<string | null | undefined> {
	if (!details) return null;
	const hasConnection = details.connection !== null;
	const fixtures = details.fixtures;
	if (!hasConnection && fixtures.length === 0) return null;
	if (hasConnection && fixtures.length === 0) return null;
	type Item = vscode.QuickPickItem & {
		mode: "live" | "fixture";
		name?: string;
	};
	const items: Item[] = [];
	if (hasConnection) {
		items.push({
			label: `connection: ${details.connection}`,
			mode: "live",
		});
	}
	for (const f of fixtures) {
		items.push({
			label: `fixture: ${f}`,
			mode: "fixture",
			name: f,
		});
	}
	const picked = await vscode.window.showQuickPick(items, {
		placeHolder: `Run ${projectionName} against...`,
	});
	if (!picked) return undefined;
	return picked.mode === "live" ? null : (picked.name ?? null);
}
