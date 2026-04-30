import * as vscode from "vscode";
import { exec } from "node:child_process";
import type { Manifest } from "../types.js";

type Logger = (msg: string) => void;

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

	buildCommand(args: string): string {
		const template = vscode.workspace
			.getConfiguration("gaffer")
			.get<string>("command", "gaffer");
		if (template.includes("{command}")) {
			return template.replace("{command}", args);
		}
		return `${template} ${args}`;
	}

	async fetchManifest(cwd: string | undefined): Promise<Manifest> {
		const command = this.buildCommand("manifest");
		try {
			const output = await execAsync(command, { cwd });
			const manifest = JSON.parse(output) as Manifest;
			this._manifest = manifest;
			this._log(`Manifest loaded (v${manifest.version})`);
			return manifest;
		} catch (err) {
			const msg = err instanceof Error ? err.message : String(err);
			this._log(`Manifest fetch failed: ${msg}`);
			this._manifest = null;
			throw err;
		}
	}
}

function execAsync(
	command: string,
	options: { cwd?: string } = {},
): Promise<string> {
	return new Promise((resolve, reject) => {
		const shell = process.env["SHELL"];
		const shellCmd = shell
			? `${shell} -i -c ${JSON.stringify(command)}`
			: command;
		exec(shellCmd, { ...options, timeout: 10_000 }, (err, stdout, stderr) => {
			if (err) {
				const stderrSuffix = stderr ? ` (stderr: ${stderr.trim()})` : "";
				reject(new Error(`${err.message}${stderrSuffix}`));
			} else {
				resolve(stdout);
			}
		});
	});
}
