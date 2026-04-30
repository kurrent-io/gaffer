import { spawn, type ChildProcess } from "node:child_process";
import readline from "node:readline";
import stripAnsi from "strip-ansi";
import type { CliMessage, CliMessageType } from "../types.js";

export interface ProcessOptions {
	log?: ((msg: string) => void) | undefined;
	cwd?: string | undefined;
}

type LineHandler = (msg: CliMessage) => void;
type ExitHandler = (code: number | null) => void;

export class GafferProcess {
	private readonly _argv: string[];
	private readonly _cwd: string | undefined;
	private readonly _log: (msg: string) => void;
	private _proc: ChildProcess | null = null;
	private _onLine: LineHandler = () => {};
	private _onExit: ExitHandler = () => {};

	constructor(argv: string[], options: ProcessOptions = {}) {
		if (argv.length === 0) throw new Error("argv must not be empty");
		this._argv = argv;
		this._cwd = options.cwd;
		this._log = options.log ?? (() => {});
	}

	start(): this {
		this._log(
			`Spawning: ${this._argv.map((a) => JSON.stringify(a)).join(" ")}` +
				(this._cwd ? ` (cwd: ${this._cwd})` : ""),
		);

		const proc = spawn(this._argv[0]!, this._argv.slice(1), {
			stdio: ["ignore", "pipe", "pipe"],
			cwd: this._cwd,
			shell: false,
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

			// `restore` is declared below; the setTimeout callback fires async,
			// so it runs after the `const restore` initializer.
			const timer = setTimeout(() => {
				restore();
				reject(new Error(`Timeout waiting for "${type}" message`));
			}, timeoutMs);

			const restore = (): void => {
				this._onLine = prevLine;
				this._onExit = prevExit;
				clearTimeout(timer);
			};

			this._onExit = (code) => {
				restore();
				prevExit(code);
				reject(
					new Error(`Process exited (code ${code}) before "${type}" message`),
				);
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
