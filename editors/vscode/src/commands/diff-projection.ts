// The "Diff against deployed" action from the projection action menu: run
// `gaffer diff <name> --env <env> --json`, then open VS Code's native diff
// editor with the deployed and local source as read-only virtual documents.
//
// The two sides are served by GafferDiffContentProvider under the `gaffer-diff:`
// scheme - a source string keyed by (env, projection, side), refreshed in place
// so re-running a diff updates an already-open editor. The CLI already resolves
// refs, dials, and returns both canonical sources (UI-1826), so this is a spawn,
// not a second implementation of the comparison.

import * as vscode from "vscode";
import * as v from "valibot";
import { log } from "../output.js";

export const GAFFER_DIFF_SCHEME = "gaffer-diff";

// A diff side as `gaffer diff --json` reports it. Only source is needed to
// render; ref/hash are validated so a shape change is caught at the boundary.
const DiffSideSchema = v.object({
	ref: v.string(),
	hash: v.optional(v.string()),
	source: v.optional(v.string(), ""),
});

// The subset of the `gaffer diff --json` payload the editor consumes: both
// sides' source and the drift verdict (to tell "not deployed" from a real
// diff). Unmodelled fields (lines, changes, provenance) pass through ignored.
const DiffJsonSchema = v.object({
	name: v.string(),
	left: DiffSideSchema,
	right: DiffSideSchema,
	verdict: v.optional(v.object({ drift: v.optional(v.string()) })),
});

type DiffJson = v.InferOutput<typeof DiffJsonSchema>;

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

	// URI path carries a human-readable label (shown on each side's tab) plus
	// the env, so the two sides are distinct documents that don't collide across
	// environments. encodeURIComponent guards a projection name with a slash.
	#uri(name: string, env: string, side: "deployed" | "local"): vscode.Uri {
		const label =
			side === "deployed" ? `${name} (deployed)` : `${name} (local)`;
		return vscode.Uri.from({
			scheme: GAFFER_DIFF_SCHEME,
			path: `/${encodeURIComponent(env)}/${encodeURIComponent(label)}`,
		});
	}
}

export interface DiffProjectionArgs {
	name: string;
	tomlUri: vscode.Uri;
	env: string;
}

export interface DiffProjectionDeps {
	// Runs the CLI and returns its stdout or the failure (with err.cause.stderr
	// attached), mirroring runGafferCommand's result shape.
	run: (
		args: string[],
		cwd: string,
	) => Promise<{ ok: true; stdout: string } | { ok: false; err: unknown }>;
	provider: GafferDiffContentProvider;
}

// The CLI's typed sign-in error (target.AuthRequiredError) renders as
// `env "x" requires sign-in: run \`gaffer auth --env x\``. Match the stable
// phrase so an auth failure offers a one-click sign-in rather than a bare error;
// a miss degrades to the generic error path, which still shows that same
// actionable text.
function stderrOf(err: unknown): string {
	const cause = (err as { cause?: { stderr?: unknown } })?.cause;
	return typeof cause?.stderr === "string" ? cause.stderr : "";
}

export function diffProjection(
	deps: DiffProjectionDeps,
): (args: DiffProjectionArgs) => Promise<void> {
	return async ({ name, tomlUri, env }) => {
		if (!vscode.workspace.isTrusted) return;
		const cwd = vscode.Uri.joinPath(tomlUri, "..").fsPath;
		const result = await deps.run(["diff", name, "--env", env, "--json"], cwd);
		if (!result.ok) {
			await reportFailure(name, env, tomlUri, result.err);
			return;
		}

		let json: DiffJson;
		try {
			json = v.parse(DiffJsonSchema, JSON.parse(result.stdout));
		} catch (err) {
			log(`diff: unparseable --json output: ${String(err)}`);
			await vscode.window.showErrorMessage(
				`Couldn't read the diff for "${name}".`,
			);
			return;
		}

		// No deployed side: the projection isn't on this env yet. A diff against
		// an empty document would misrepresent "not deployed" as "everything
		// removed", so say it plainly instead.
		if (json.verdict?.drift === "not-deployed" || json.left.source === "") {
			await vscode.window.showInformationMessage(
				`"${name}" isn't deployed to ${env}.`,
			);
			return;
		}

		const { left, right } = deps.provider.setSides(
			name,
			env,
			json.left.source,
			json.right.source,
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
	const stderr = stderrOf(err);
	if (stderr.includes("requires sign-in")) {
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
	const detail = stderr || (err instanceof Error ? err.message : String(err));
	log(`diff ${name} --env ${env} failed: ${detail}`);
	await vscode.window.showErrorMessage(
		`Couldn't diff "${name}" against ${env}: ${detail}`,
	);
}
