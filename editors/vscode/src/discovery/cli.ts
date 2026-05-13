import * as vscode from "vscode";
import { execFile } from "node:child_process";
import * as v from "valibot";
import { log } from "../output.js";
import { ManifestSchema, type Manifest } from "./schemas.js";

const DEFAULT_COMMAND: readonly string[] = ["gaffer"];

/**
 * Build the argv to invoke gaffer with the given subcommand args.
 *
 * Reads `gaffer.command` only from User scope - workspace and folder scope
 * are ignored as a defense against hostile workspaces overriding the
 * binary path. Falls back to `["gaffer"]` (resolved from PATH).
 */
export function buildGafferArgv(args: string[]): string[] {
	const inspected = vscode.workspace
		.getConfiguration("gaffer")
		.inspect<string[]>("command");
	const userValue = inspected?.globalValue;
	const prefix =
		Array.isArray(userValue) && userValue.length > 0
			? userValue
			: [...DEFAULT_COMMAND];
	return [...prefix, ...args];
}

/**
 * Fetch and parse the gaffer CLI manifest. Returns `null` rather than
 * throwing - the manifest can legitimately fail to load (CLI not
 * installed, workspace untrusted, bad config) and the rest of the
 * extension is built to handle a null manifest. `onError` fires for
 * actual fetch failures (CLI missing, parse errors); trust-skip is
 * silent.
 */
export async function tryFetchManifest(
	cwd: string | undefined,
	onError?: (err: unknown) => void,
): Promise<Manifest | null> {
	if (!vscode.workspace.isTrusted) {
		log("workspace untrusted, skipping manifest fetch");
		return null;
	}
	const argv = buildGafferArgv(["manifest"]);
	try {
		const opts: { cwd?: string } = {};
		if (cwd !== undefined) opts.cwd = cwd;
		const output = await execFileAsync(argv, opts);
		const raw: unknown = JSON.parse(output);
		const parsed = v.safeParse(ManifestSchema, raw);
		if (!parsed.success) {
			throw new Error(
				`malformed manifest: ${parsed.issues.map((i) => i.message).join("; ")}`,
			);
		}
		log(`Manifest loaded (v${parsed.output.version})`);
		return parsed.output;
	} catch (err) {
		const msg = err instanceof Error ? err.message : String(err);
		log(`Manifest fetch failed: ${msg}`);
		onError?.(err);
		return null;
	}
}

export const hasCommand = (m: Manifest | null, name: string): boolean =>
	m?.commands?.[name] != null;

export const hasFlag = (
	m: Manifest | null,
	command: string,
	flag: string,
): boolean => m?.commands?.[command]?.flags?.includes(flag) ?? false;

function execFileAsync(
	argv: string[],
	options: { cwd?: string } = {},
): Promise<string> {
	return new Promise((resolve, reject) => {
		const [head, ...rest] = argv;
		if (!head) {
			reject(new Error("argv must not be empty"));
			return;
		}
		const execOpts: { cwd?: string; timeout: number; shell: false } = {
			timeout: 10_000,
			shell: false,
		};
		if (options.cwd !== undefined) execOpts.cwd = options.cwd;
		execFile(head, rest, execOpts, (err, stdout, stderr) => {
			if (err) {
				// Augment in place to keep err.code / err.killed accessible
				// to callers that need to classify the failure (e.g.
				// telemetry's classifyManifestError). Wrapping in a fresh
				// Error would lose those fields.
				if (stderr) err.message = `${err.message} (stderr: ${stderr.trim()})`;
				reject(err);
			} else {
				resolve(stdout);
			}
		});
	});
}
