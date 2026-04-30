import { spawn, type ChildProcess } from "node:child_process";
import readline from "node:readline";
import * as v from "valibot";
import stripAnsi from "strip-ansi";
import {
	CliMessageWireSchema,
	type CliMessage,
	type CliMessageType,
} from "../types.js";

export interface ProcessOptions {
	log?: ((msg: string) => void) | undefined;
	cwd?: string | undefined;
}

type LineHandler = (msg: CliMessage) => void;
type ExitHandler = (code: number | null) => void;

export class GafferProcess {
	readonly #argv: string[];
	readonly #cwd: string | undefined;
	readonly #log: (msg: string) => void;
	#proc: ChildProcess | null = null;
	#onLine: LineHandler = () => {};
	#onExit: ExitHandler = () => {};

	constructor(argv: string[], options: ProcessOptions = {}) {
		if (argv.length === 0) throw new Error("argv must not be empty");
		this.#argv = argv;
		this.#cwd = options.cwd;
		this.#log = options.log ?? (() => {});
	}

	start(): this {
		const [head, ...rest] = this.#argv;
		// Constructor validates argv is non-empty.
		if (head === undefined) throw new Error("argv must not be empty");

		this.#log(
			`Spawning: ${this.#argv.map((a) => JSON.stringify(a)).join(" ")}` +
				(this.#cwd ? ` (cwd: ${this.#cwd})` : ""),
		);

		const proc = spawn(head, rest, {
			stdio: ["ignore", "pipe", "pipe"],
			cwd: this.#cwd,
			shell: false,
		});
		this.#proc = proc;

		if (!proc.stdout || !proc.stderr) {
			throw new Error("spawn returned a process without piped stdout/stderr");
		}

		const rl = readline.createInterface({ input: proc.stdout });
		rl.on("line", (line) => {
			let raw: unknown;
			try {
				raw = JSON.parse(line);
			} catch {
				this.#log(`[stdout] ${line}`);
				return;
			}
			const result = v.safeParse(CliMessageWireSchema, raw);
			if (result.success) {
				this.#onLine(result.output);
			} else {
				this.#log(`[stdout] ${line}`);
			}
		});

		proc.stderr.on("data", (data: Buffer) => {
			const text = stripAnsi(data.toString()).trim();
			if (text) this.#log(`[stderr] ${text}`);
		});

		proc.on("exit", (code) => {
			this.#log(`Process exited with code ${code}`);
			this.#onExit(code);
		});

		return this;
	}

	onLine(fn: LineHandler): this {
		this.#onLine = fn;
		return this;
	}

	onExit(fn: ExitHandler): this {
		this.#onExit = fn;
		return this;
	}

	waitForMessage<T extends CliMessageType>(
		type: T,
		timeoutMs = 15000,
	): Promise<Extract<CliMessage, { type: T }>> {
		return new Promise((resolve, reject) => {
			const prevLine = this.#onLine;
			const prevExit = this.#onExit;

			// `restore` is declared below; the setTimeout callback fires async,
			// so it runs after the `const restore` initializer.
			const timer = setTimeout(() => {
				restore();
				reject(new Error(`Timeout waiting for "${type}" message`));
			}, timeoutMs);

			const restore = (): void => {
				this.#onLine = prevLine;
				this.#onExit = prevExit;
				clearTimeout(timer);
			};

			this.#onExit = (code) => {
				restore();
				prevExit(code);
				reject(
					new Error(`Process exited (code ${code}) before "${type}" message`),
				);
			};

			this.#onLine = (msg) => {
				prevLine(msg);
				if (msg.type === type) {
					restore();
					resolve(msg as Extract<CliMessage, { type: T }>);
				}
			};
		});
	}

	kill(): void {
		if (this.#proc && !this.#proc.killed) {
			this.#proc.kill();
		}
	}
}
