import { spawn, type ChildProcess } from "node:child_process";
import readline from "node:readline";
import type { CliMessage, CliMessageType } from "../types.js";

export interface ProcessOptions {
	log?: (msg: string) => void;
	cwd?: string;
}

type LineHandler = (msg: CliMessage) => void;
type ExitHandler = (code: number | null) => void;

const ansiRegex = /\x1b\[[0-9;]*m/g;
const stripAnsi = (s: string) => s.replace(ansiRegex, "");

export class GafferProcess {
	private readonly _command: string;
	private readonly _cwd: string | undefined;
	private readonly _log: (msg: string) => void;
	private _proc: ChildProcess | null = null;
	private _onLine: LineHandler = () => {};
	private _onExit: ExitHandler = () => {};

	constructor(command: string, options: ProcessOptions = {}) {
		this._command = command;
		this._cwd = options.cwd;
		this._log = options.log ?? (() => {});
	}

	start(): this {
		const shell = process.env["SHELL"] ?? "/bin/sh";
		this._log(
			`Spawning: ${shell} -c ${JSON.stringify(this._command)}` +
				(this._cwd ? ` (cwd: ${this._cwd})` : ""),
		);

		const proc = spawn(shell, ["-c", this._command], {
			stdio: ["ignore", "pipe", "pipe"],
			cwd: this._cwd,
		});
		this._proc = proc;

		if (!proc.stdout || !proc.stderr) {
			throw new Error("spawn returned a process without piped stdout/stderr");
		}

		const rl = readline.createInterface({ input: proc.stdout });
		rl.on("line", (line) => {
			try {
				const msg = JSON.parse(line) as CliMessage;
				this._onLine(msg);
			} catch {
				this._log(`[stdout] ${line}`);
			}
		});

		proc.stderr.on("data", (data: Buffer) => {
			const text = stripAnsi(data.toString()).trim();
			if (text) this._log(`[stderr] ${text}`);
		});

		proc.on("exit", (code) => {
			this._log(`Process exited with code ${code}`);
			this._onExit(code);
		});

		return this;
	}

	onLine(fn: LineHandler): this {
		this._onLine = fn;
		return this;
	}

	onExit(fn: ExitHandler): this {
		this._onExit = fn;
		return this;
	}

	waitForMessage<T extends CliMessageType>(
		type: T,
		timeoutMs = 15000,
	): Promise<Extract<CliMessage, { type: T }>> {
		return new Promise((resolve, reject) => {
			const prevLine = this._onLine;
			const prevExit = this._onExit;

			let timer: NodeJS.Timeout;
			const restore = () => {
				this._onLine = prevLine;
				this._onExit = prevExit;
				clearTimeout(timer);
			};

			timer = setTimeout(() => {
				restore();
				reject(new Error(`Timeout waiting for "${type}" message`));
			}, timeoutMs);

			this._onExit = (code) => {
				restore();
				prevExit(code);
				reject(new Error(`Process exited (code ${code}) before "${type}" message`));
			};

			this._onLine = (msg) => {
				prevLine(msg);
				if (msg.type === type) {
					restore();
					resolve(msg as Extract<CliMessage, { type: T }>);
				}
			};
		});
	}

	kill(): void {
		if (this._proc && !this._proc.killed) {
			this._proc.kill();
		}
	}
}
