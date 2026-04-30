import * as vscode from "vscode";
import { execFile } from "node:child_process";
import * as v from "valibot";
import { ManifestSchema, type Manifest } from "./schemas.js";

type Logger = (msg: string) => void;

const DEFAULT_COMMAND: readonly string[] = ["./bin/gaffer"];

export class GafferCli {
	readonly #log: Logger;
	#manifest: Manifest | null = null;

	constructor(log?: Logger) {
		this.#log = log ?? (() => {});
	}

	get manifest(): Manifest | null {
		return this.#manifest;
	}

	hasCommand(name: string): boolean {
		return this.#manifest?.commands?.[name] != null;
	}

	hasFlag(command: string, flag: string): boolean {
		return this.#manifest?.commands?.[command]?.flags?.includes(flag) ?? false;
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
			this.#manifest = parsed.output;
			this.#log(`Manifest loaded (v${parsed.output.version})`);
			return parsed.output;
		} catch (err) {
			const msg = err instanceof Error ? err.message : String(err);
			this.#log(`Manifest fetch failed: ${msg}`);
			this.#manifest = null;
			throw err;
		}
	}
}

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
				const stderrSuffix = stderr ? ` (stderr: ${stderr.trim()})` : "";
				reject(new Error(`${err.message}${stderrSuffix}`));
			} else {
				resolve(stdout);
			}
		});
	});
}
