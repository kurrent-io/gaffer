import { GafferProcess, type ProcessOptions } from "./process.js";
import { renderCliMessage } from "./output-renderer.js";
import { clearOutput, writeOutput } from "../output.js";
import type { CliMessage } from "./schemas.js";

export interface SessionOptions {
	cwd?: string;
}

type CliMessageType = CliMessage["type"];
type Listener<T extends CliMessageType = CliMessageType> = (
	msg: Extract<CliMessage, { type: T }>,
) => void;
type AnyListener = (msg: CliMessage) => void;

// Surface SessionController consumes. Extracted as an interface so
// tests can substitute a fake without spawning a subprocess. The
// production implementation is GafferSession below; the
// SessionController takes a `createSession` factory in its deps so
// the test seam is a one-line override.
export interface SessionLike {
	readonly name: string;
	on<T extends CliMessageType>(type: T, fn: Listener<T>): this;
	on(type: "*", fn: AnyListener): this;
	start(): this;
	waitForDebug(): Promise<Extract<CliMessage, { type: "debug" }>>;
	stop(): void;
	dispose(): void;
}

export type CreateSession = (
	name: string,
	argv: string[],
	options?: SessionOptions,
) => SessionLike;

export const createGafferSession: CreateSession = (name, argv, options) =>
	new GafferSession(name, argv, options);

export class GafferSession implements SessionLike {
	readonly #name: string;
	readonly #argv: string[];
	readonly #cwd: string | undefined;
	readonly #listeners = new Map<CliMessageType | "*", AnyListener[]>();
	#proc: GafferProcess | null = null;

	constructor(name: string, argv: string[], options: SessionOptions = {}) {
		this.#name = name;
		this.#argv = argv;
		this.#cwd = options.cwd;
		// Channel is a module-level singleton (output.ts), reused across
		// sessions. Clear on construction and write the session header so
		// subsequent renderCliMessage calls land under it.
		clearOutput();
		writeOutput(`=== ${name} ===`);
	}

	get name(): string {
		return this.#name;
	}

	on<T extends CliMessageType>(type: T, fn: Listener<T>): this;
	on(type: "*", fn: AnyListener): this;
	on(type: CliMessageType | "*", fn: AnyListener): this {
		const list = this.#listeners.get(type);
		if (list) {
			list.push(fn);
		} else {
			this.#listeners.set(type, [fn]);
		}
		return this;
	}

	start(): this {
		const opts: ProcessOptions = {};
		if (this.#cwd !== undefined) opts.cwd = this.#cwd;
		const proc = new GafferProcess(this.#argv, opts);
		this.#proc = proc;

		proc.onLine((msg) => this.#dispatch(msg));
		proc.onExit((code) => this.#dispatch({ type: "exit", code }));

		proc.start();
		return this;
	}

	async waitForDebug(): Promise<Extract<CliMessage, { type: "debug" }>> {
		if (!this.#proc) throw new Error("session not started");
		return this.#proc.waitForMessage("debug");
	}

	stop(): void {
		if (this.#proc) {
			// Detach line/exit handlers before kill so any buffered stdout
			// flushed between SIGTERM and process exit can't dispatch into a
			// shared output channel that the next session has already cleared.
			this.#proc.onLine(() => {});
			this.#proc.onExit(() => {});
			this.#proc.kill();
			this.#proc = null;
		}
	}

	dispose(): void {
		this.stop();
		this.#listeners.clear();
	}

	#dispatch(msg: CliMessage): void {
		renderCliMessage(msg, writeOutput);
		for (const fn of this.#listeners.get(msg.type) ?? []) fn(msg);
		for (const fn of this.#listeners.get("*") ?? []) fn(msg);
	}
}
