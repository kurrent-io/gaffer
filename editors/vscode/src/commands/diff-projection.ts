// The "Diff against deployed" action from the projection action menu: ask the
// language server for a projection's deployed↔local diff, then open VS Code's
// native diff editor with the deployed and local source as read-only virtual
// documents.
//
// The two sides are served by GafferDiffContentProvider under the `gaffer-diff:`
// scheme - a source string keyed by (env, projection, side), refreshed in place
// so re-running a diff updates an already-open editor. The diff is computed by
// the server over its warm per-env connection (gaffer/diffProjection), so a
// re-diff is one read RPC rather than a cold `gaffer diff` spawn.

import * as vscode from "vscode";
import { log } from "../output.js";
import { type ProjectionDiff } from "../lsp/diff.js";
import { LspAuthRequiredError } from "../lsp/request.js";

export const GAFFER_DIFF_SCHEME = "gaffer-diff";

// Serves the two diff sides as read-only virtual documents. One instance is
// registered for the whole session; each diff invocation stores its sources and
// hands back the pair of URIs to open.
export class GafferDiffContentProvider
	implements vscode.TextDocumentContentProvider, vscode.Disposable
{
	readonly #onDidChange = new vscode.EventEmitter<vscode.Uri>();
	readonly onDidChange = this.#onDidChange.event;
	readonly #contents = new Map<string, string>();

	dispose(): void {
		this.#onDidChange.dispose();
		this.#contents.clear();
	}

	provideTextDocumentContent(uri: vscode.Uri): string {
		return this.#contents.get(uri.toString()) ?? "";
	}

	// Store both sides and return their URIs. Keyed by (env, projection, side)
	// so the same diff reuses its documents; firing onDidChange refreshes an
	// editor already showing them.
	setSides(
		name: string,
		env: string,
		deployedSource: string,
		localSource: string,
	): { left: vscode.Uri; right: vscode.Uri } {
		const left = this.#uri(name, env, "deployed");
		const right = this.#uri(name, env, "local");
		this.#contents.set(left.toString(), deployedSource);
		this.#contents.set(right.toString(), localSource);
		this.#onDidChange.fire(left);
		this.#onDidChange.fire(right);
		return { left, right };
	}

	// The path ends in `.js` so VS Code infers JavaScript and syntax-highlights
	// both sides (projection sources are JS today). env and name are separate,
	// encoded segments so a slash or special char in either can't collide two
	// distinct (env, name, side) tuples or break out of its segment; the side is
	// the final `deployed.js` / `local.js` segment, which reads as each pane's
	// filename.
	#uri(name: string, env: string, side: "deployed" | "local"): vscode.Uri {
		return vscode.Uri.from({
			scheme: GAFFER_DIFF_SCHEME,
			path: `/${encodeURIComponent(env)}/${encodeURIComponent(name)}/${side}.js`,
		});
	}
}

export interface DiffProjectionArgs {
	name: string;
	tomlUri: vscode.Uri;
	env: string;
}

export interface DiffProjectionDeps {
	// Fetches the projection's deployed↔local diff over the LSP warm connection.
	// Throws LspAuthRequiredError when the env needs sign-in, LspUnavailableError
	// otherwise. A field so tests inject a fake in place of a live server.
	requestDiff: (
		name: string,
		tomlUri: vscode.Uri,
		env: string,
	) => Promise<ProjectionDiff>;
	provider: GafferDiffContentProvider;
}

export function diffProjection(
	deps: DiffProjectionDeps,
): (args: DiffProjectionArgs) => Promise<void> {
	return async ({ name, tomlUri, env }) => {
		if (!vscode.workspace.isTrusted) return;

		let diff: ProjectionDiff;
		try {
			diff = await deps.requestDiff(name, tomlUri, env);
		} catch (err) {
			await reportFailure(name, env, tomlUri, err);
			return;
		}

		// No deployed side: the projection isn't on this env yet. A diff against
		// an empty document would misrepresent "not deployed" as "everything
		// removed", so say it plainly instead.
		if (diff.verdict?.drift === "not-deployed" || diff.left.source === "") {
			await vscode.window.showInformationMessage(
				`"${name}" isn't deployed to ${env}.`,
			);
			return;
		}

		const { left, right } = deps.provider.setSides(
			name,
			env,
			diff.left.source,
			diff.right.source,
		);
		await vscode.commands.executeCommand(
			"vscode.diff",
			left,
			right,
			`${name}: deployed ↔ local (${env})`,
		);
	};
}

async function reportFailure(
	name: string,
	env: string,
	tomlUri: vscode.Uri,
	err: unknown,
): Promise<void> {
	if (err instanceof LspAuthRequiredError) {
		const signIn = "Sign in";
		const choice = await vscode.window.showErrorMessage(
			`${env} needs sign-in to diff "${name}".`,
			signIn,
		);
		if (choice === signIn) {
			await vscode.commands.executeCommand("gaffer.signIn", { env, tomlUri });
		}
		return;
	}
	const detail = err instanceof Error ? err.message : String(err);
	log(`diff ${name} --env ${env} failed: ${detail}`);
	await vscode.window.showErrorMessage(
		`Couldn't diff "${name}" against ${env}: ${detail}`,
	);
}
