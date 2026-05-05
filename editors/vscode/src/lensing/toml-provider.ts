import * as vscode from "vscode";
import path from "node:path";
import {
	classifyFixtures,
	projectionBlocks,
	scanLines,
} from "../discovery/project-index.js";
import { log } from "../output.js";
import { setTomlDiagnostics } from "../diagnostics.js";
import { buildInvalidFixtureLens, buildLens, buildPickLens } from "./lens.js";
import type { Manifest } from "../discovery/schemas.js";
import type { DebugState } from "../types.js";

export class TomlCodeLensProvider implements vscode.CodeLensProvider {
	readonly #onDidChange = new vscode.EventEmitter<void>();
	readonly onDidChangeCodeLenses = this.#onDidChange.event;
	#debugState: DebugState = { name: null, status: "idle" };
	#manifest: Manifest | null;

	constructor(initialManifest: Manifest | null) {
		this.#manifest = initialManifest;
	}

	setManifest(manifest: Manifest | null): void {
		this.#manifest = manifest;
		this.#onDidChange.fire();
	}

	// Lens-side copy of the controller's debug state. Pushed via the
	// SessionController's pushDebugState callback rather than a shared
	// reference so the controller's mutations don't reach in here.
	setDebugState(state: Readonly<DebugState>): void {
		this.#debugState = { ...state };
		this.#onDidChange.fire();
	}

	refresh(): void {
		this.#onDidChange.fire();
	}

	// Sole writer of the gaffer-toml DiagnosticCollection for this
	// document.uri - every return path calls setTomlDiagnostics so a
	// previously-bad toml that becomes valid stops squiggling. Adding
	// another writer (a different provider, a separate watcher) would
	// race; route any future toml validation through here.
	provideCodeLenses(document: vscode.TextDocument): vscode.CodeLens[] {
		const text = document.getText();
		const blocks = projectionBlocks(text);
		if (blocks.length === 0) {
			setTomlDiagnostics(document.uri, []);
			return [];
		}

		const scan = scanLines(text);
		const tomlUri = document.uri;
		const tomlDir = path.dirname(document.uri.fsPath);

		const lenses: vscode.CodeLens[] = [];
		const diagnostics: vscode.Diagnostic[] = [];
		// Walk projection headers + parsed blocks in lockstep. If the
		// counts don't line up the source is malformed (or our scanner
		// missed something) - skip the file rather than mis-attribute.
		if (blocks.length !== scan.projectionHeaders.length) {
			log(
				`Lens skipped for ${document.uri.fsPath}: ${blocks.length} projection blocks vs ${scan.projectionHeaders.length} headers (malformed toml or unhandled syntax)`,
			);
			setTomlDiagnostics(document.uri, []);
			return [];
		}

		for (const [i, block] of blocks.entries()) {
			if (!block) continue;
			const projHeader = scan.projectionHeaders[i];
			if (!projHeader) continue;
			const projRange = new vscode.Range(
				projHeader.line,
				0,
				projHeader.line,
				projHeader.length,
			);
			const projLens = buildLens(
				this.#manifest,
				this.#debugState,
				block.name,
				projRange,
				tomlUri,
			);
			if (projLens) lenses.push(projLens);

			const statuses = classifyFixtures(block.name, block.fixtures, tomlDir);
			const validFixtureNames = statuses
				.filter((s) => s.kind === "valid")
				.map((s) => s.name);

			// Dropdown lens on the projection header so any fixture form
			// (dotted keys, inline table, [projection.fixtures] table)
			// still gets a working "Debug from fixture..." entry point.
			// Per-fixture line lenses below are an addition, not a
			// replacement, when the dotted-key form is used.
			if (validFixtureNames.length > 0) {
				const pickLens = buildPickLens(
					this.#manifest,
					this.#debugState,
					block.name,
					projRange,
					tomlUri,
					validFixtureNames,
				);
				if (pickLens) lenses.push(pickLens);
			}

			// Per-fixture lenses on each `fixtures.<name> = "..."` line
			// belonging to this projection (between this header and the
			// next). Validation status comes from classifyFixtures so the
			// invalid-fixture warning lens stays correct.
			const nextProjLine =
				scan.projectionHeaders[i + 1]?.line ?? Number.POSITIVE_INFINITY;
			const fixtureLines = scan.fixtureLines.filter(
				(h) => h.line > projHeader.line && h.line < nextProjLine,
			);
			const statusByName = new Map(statuses.map((s) => [s.name, s] as const));
			const fixtureLineByName = new Map(
				fixtureLines.map((fl) => [fl.name, fl] as const),
			);
			for (const fl of fixtureLines) {
				const status = statusByName.get(fl.name);
				if (!status) continue;
				const range = new vscode.Range(fl.line, 0, fl.line, fl.length);
				if (status.kind === "valid") {
					const lens = buildLens(
						this.#manifest,
						this.#debugState,
						block.name,
						range,
						tomlUri,
						status.name,
					);
					if (lens) lenses.push(lens);
				} else {
					lenses.push(buildInvalidFixtureLens(range, status.reason));
				}
			}

			// Diagnostics for every invalid fixture, anchored to its
			// fixtures.<name> line when we have one (dotted-key form);
			// fall back to the projection header otherwise (inline-table
			// or [projection.fixtures] table form, where the name has no
			// dedicated source line we can scan to).
			for (const status of statuses) {
				if (status.kind !== "invalid") continue;
				const fl = fixtureLineByName.get(status.name ?? "");
				const diagRange = fl
					? new vscode.Range(fl.line, 0, fl.line, fl.length)
					: projRange;
				const diag = new vscode.Diagnostic(
					diagRange,
					`Invalid fixture${status.name ? ` "${status.name}"` : ""}: ${status.reason}`,
					vscode.DiagnosticSeverity.Error,
				);
				diag.source = "gaffer";
				diagnostics.push(diag);
			}
		}
		setTomlDiagnostics(document.uri, diagnostics);
		return lenses;
	}
}
