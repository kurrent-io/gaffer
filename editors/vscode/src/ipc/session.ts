import * as vscode from "vscode";
import { GafferProcess, type ProcessOptions } from "./process.js";
import { renderCliMessage } from "./output-renderer.js";
import type { CliMessage } from "./schemas.js";

export interface SessionOptions {
	log?: (msg: string) => void;
	cwd?: string;
}

type CliMessageType = CliMessage["type"];
type Listener<T extends CliMessageType = CliMessageType> = (
	msg: Extract<CliMessage, { type: T }>,
) => void;
type AnyListener = (msg: CliMessage) => void;

export class GafferSession {
	readonly #name: string;
	readonly #argv: string[];
	readonly #log: (msg: string) => void;
	readonly #cwd: string | undefined;
	readonly #listeners = new Map<CliMessageType | "*", AnyListener[]>();
	readonly #output: vscode.OutputChannel;
	#proc: GafferProcess | null = null;

	constructor(name: string, argv: string[], options: SessionOptions = {}) {
		this.#name = name;
		this.#argv = argv;
		this.#log = options.log ?? (() => {});
		this.#cwd = options.cwd;
		this.#output = vscode.window.createOutputChannel(`Gaffer: ${name}`, "log");
	}

	get name(): string {
		return this.#name;
	}

	get output(): vscode.OutputChannel {
		return this.#output;
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
		const opts: ProcessOptions = { log: this.#log };
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
			this.#proc.kill();
			this.#proc = null;
		}
	}

	dispose(): void {
		this.stop();
		this.#output.dispose();
		this.#listeners.clear();
	}

	#dispatch(msg: CliMessage): void {
		renderCliMessage(this.#output, msg);
		for (const fn of this.#listeners.get(msg.type) ?? []) fn(msg);
		for (const fn of this.#listeners.get("*") ?? []) fn(msg);
	}
}
