import { spawn, type ChildProcess } from "node:child_process";
import readline from "node:readline";
import * as v from "valibot";
import stripAnsi from "strip-ansi";
import { log } from "../output.js";
import {
	CliMessageWireSchema,
	type CliMessage,
	type CliMessageType,
} from "./schemas.js";

export interface ProcessOptions {
	cwd?: string;
	env?: NodeJS.ProcessEnv;
}

type LineHandler = (msg: CliMessage) => void;
type ExitHandler = (code: number | null) => void;

export class GafferProcess {
	readonly #argv: string[];
	readonly #cwd: string | undefined;
	readonly #env: NodeJS.ProcessEnv | undefined;
	#proc: ChildProcess | null = null;
	#onLine: LineHandler = () => {};
	#onExit: ExitHandler = () => {};

	constructor(argv: string[], options: ProcessOptions = {}) {
		if (argv.length === 0) throw new Error("argv must not be empty");
		this.#argv = argv;
		this.#cwd = options.cwd;
		this.#env = options.env;
	}

	start(): this {
		const [head, ...rest] = this.#argv;
		// Constructor validates argv is non-empty.
		if (head === undefined) throw new Error("argv must not be empty");

		log(
			`Spawning: ${this.#argv.map((a) => JSON.stringify(a)).join(" ")}` +
				(this.#cwd ? ` (cwd: ${this.#cwd})` : ""),
		);

		const proc = spawn(head, rest, {
			stdio: ["ignore", "pipe", "pipe"],
			cwd: this.#cwd,
			...(this.#env !== undefined && { env: this.#env }),
			shell: false,
			// POSIX: become group leader so kill(-pid) takes the whole tree.
			// Windows ignores this; tree-kill uses taskkill /T instead.
			detached: process.platform !== "win32",
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
				log(`[stdout] ${line}`);
				return;
			}
			const result = v.safeParse(CliMessageWireSchema, raw);
			if (result.success) {
				this.#onLine(result.output);
			} else {
				log(`[stdout] ${line}`);
			}
		});

		proc.stderr.on("data", (data: Buffer) => {
			const text = stripAnsi(data.toString()).trim();
			if (text) log(`[stderr] ${text}`);
		});

		// 'close' fires after stdout/stderr have been drained and parsed,
		// not just after the process exits. Critical for the fatal_error
		// path: the CLI prints a fatal_error JSON line then exits, and
		// 'exit' fires while that line is still in flight - subscribers
		// would get the exit event before the fatal_error one.
		proc.on("close", (code) => {
			log(`Process exited with code ${code}`);
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
				// Kill the child so it can't outlive the timeout regardless of
				// what the caller does with the rejected promise.
				this.kill();
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

	// Kill the child and any descendants. Plain `proc.kill()` only signals
	// the immediate child, leaving grandchildren orphaned. The CLI shells
	// out (engine, native helpers), so a leak here means real processes
	// staying behind on stop.
	kill(): void {
		const proc = this.#proc;
		if (!proc || proc.killed || proc.pid === undefined) return;
		const pid = proc.pid;
		if (process.platform === "win32") {
			const tk = spawn("taskkill", ["/pid", String(pid), "/T", "/F"], {
				stdio: "ignore",
				shell: false,
			});
			// Swallow ENOENT etc. - the process is probably gone anyway and an
			// unhandled error would crash the extension host.
			tk.on("error", () => {});
			return;
		}
		try {
			// Negative pid signals the process group (we spawned detached).
			process.kill(-pid, "SIGTERM");
		} catch {
			// Group already gone or never formed; fall back to direct kill.
			proc.kill();
		}
	}
}
