// FakeSession implements SessionLike for SessionController tests.
//
// Tests construct one via the `createSession` factory dependency:
//   const factory: CreateSession = (name, argv, opts) =>
//     fakeSession = new FakeSession(name, argv, opts);
//
// Then drive lifecycle from outside via `fire("debug", { port: 4711 })`,
// `fire("exit", { code: 0 })`, `resolveDebug(...)` / `rejectDebug(...)`.

import type { CliMessage, CliMessageType } from "../../src/ipc/schemas.js";
import type { SessionLike, SessionOptions } from "../../src/ipc/session.js";

type Listener = (msg: CliMessage) => void;

export class FakeSession implements SessionLike {
	readonly name: string;
	readonly argv: string[];
	readonly options: SessionOptions;
	startCount = 0;
	stopCount = 0;
	disposeCount = 0;
	#listeners = new Map<CliMessageType | "*", Listener[]>();
	#waitForDebug: {
		resolve: (msg: Extract<CliMessage, { type: "debug" }>) => void;
		reject: (err: unknown) => void;
	} | null = null;

	constructor(name: string, argv: string[], options: SessionOptions = {}) {
		this.name = name;
		this.argv = argv;
		this.options = options;
	}

	on(type: CliMessageType | "*", fn: Listener): this {
		const list = this.#listeners.get(type);
		if (list) list.push(fn);
		else this.#listeners.set(type, [fn]);
		return this;
	}

	start(): this {
		this.startCount++;
		return this;
	}

	waitForDebug(): Promise<Extract<CliMessage, { type: "debug" }>> {
		if (this.#waitForDebug) {
			throw new Error("waitForDebug already pending on FakeSession");
		}
		return new Promise((resolve, reject) => {
			this.#waitForDebug = { resolve, reject };
		});
	}

	resolveDebug(port: number): void {
		this.#waitForDebug?.resolve({ type: "debug", port });
		this.#waitForDebug = null;
	}

	rejectDebug(err: unknown): void {
		this.#waitForDebug?.reject(err);
		this.#waitForDebug = null;
	}

	fire(msg: CliMessage): void {
		for (const fn of this.#listeners.get(msg.type) ?? []) fn(msg);
		for (const fn of this.#listeners.get("*") ?? []) fn(msg);
	}

	stop(): void {
		this.stopCount++;
	}

	dispose(): void {
		this.disposeCount++;
		this.#listeners.clear();
	}
}
