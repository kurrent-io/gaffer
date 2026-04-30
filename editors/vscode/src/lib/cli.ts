import * as vscode from "vscode";
import { execFile } from "node:child_process";
import * as v from "valibot";
import { ManifestSchema, type Manifest } from "../types.js";

type Logger = (msg: string) => void;

const DEFAULT_COMMAND: readonly string[] = ["./bin/gaffer"];

export class GafferCli {
	private readonly _log: Logger;
	private _manifest: Manifest | null = null;

	constructor(log?: Logger) {
		this._log = log ?? (() => {});
	}

	get manifest(): Manifest | null {
		return this._manifest;
	}

	hasCommand(name: string): boolean {
		return this._manifest?.commands?.[name] != null;
	}

	hasFlag(command: string, flag: string): boolean {
		return this._manifest?.commands?.[command]?.flags?.includes(flag) ?? false;
	}

	/**
	 * Build the argv to invoke gaffer with the given subcommand args.
	 *
	 * Reads `gaffer.command` only from User scope - workspace and folder scope
	 * are ignored as a defense against hostile workspaces overriding the
	 * binary path. Falls back to `["./bin/gaffer"]`.
	 */
	buildArgv(args: string[]): string[] {
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

	async fetchManifest(cwd: string | undefined): Promise<Manifest> {
		const argv = this.buildArgv(["manifest"]);
		try {
			const output = await execFileAsync(argv, { cwd });
			const raw: unknown = JSON.parse(output);
			const parsed = v.safeParse(ManifestSchema, raw);
			if (!parsed.success) {
				throw new Error(
					`malformed manifest: ${parsed.issues.map((i) => i.message).join("; ")}`,
				);
			}
			this._manifest = parsed.output;
			this._log(`Manifest loaded (v${parsed.output.version})`);
			return parsed.output;
		} catch (err) {
			const msg = err instanceof Error ? err.message : String(err);
			this._log(`Manifest fetch failed: ${msg}`);
			this._manifest = null;
			throw err;
		}
	}
}

function execFileAsync(
	argv: string[],
	options: { cwd?: string | undefined } = {},
): Promise<string> {
	return new Promise((resolve, reject) => {
		const [head, ...rest] = argv;
		if (!head) {
			reject(new Error("argv must not be empty"));
			return;
		}
		execFile(
			head,
			rest,
			{ cwd: options.cwd, timeout: 10_000, shell: false },
			(err, stdout, stderr) => {
				if (err) {
					const stderrSuffix = stderr ? ` (stderr: ${stderr.trim()})` : "";
					reject(new Error(`${err.message}${stderrSuffix}`));
				} else {
					resolve(stdout);
				}
			},
		);
	});
}
