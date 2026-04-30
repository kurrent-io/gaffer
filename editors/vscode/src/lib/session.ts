import * as vscode from "vscode";
import { GafferProcess } from "./process.js";
import type { CliMessage } from "../types.js";

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
	private readonly _name: string;
	private readonly _argv: string[];
	private readonly _log: (msg: string) => void;
	private readonly _cwd: string | undefined;
	private readonly _listeners = new Map<CliMessageType | "*", AnyListener[]>();
	private readonly _output: vscode.OutputChannel;
	private _proc: GafferProcess | null = null;

	constructor(name: string, argv: string[], options: SessionOptions = {}) {
		this._name = name;
		this._argv = argv;
		this._log = options.log ?? (() => {});
		this._cwd = options.cwd;
		this._output = vscode.window.createOutputChannel(`Gaffer: ${name}`, "log");
	}

	get name(): string {
		return this._name;
	}

	get output(): vscode.OutputChannel {
		return this._output;
	}

	on<T extends CliMessageType>(type: T, fn: Listener<T>): this;
	on(type: "*", fn: AnyListener): this;
	on(type: CliMessageType | "*", fn: AnyListener): this {
		const list = this._listeners.get(type);
		if (list) {
			list.push(fn);
		} else {
			this._listeners.set(type, [fn]);
		}
		return this;
	}

	start(): this {
		const proc = new GafferProcess(this._argv, { log: this._log, cwd: this._cwd });
		this._proc = proc;

		proc.onLine((msg) => this._dispatch(msg));
		proc.onExit((code) => {
			this._writeOutput(`Process exited (code ${code})`);
			this._dispatch({ type: "exit", code });
		});

		proc.start();
		return this;
	}

	async waitForDebug(): Promise<Extract<CliMessage, { type: "debug" }>> {
		if (!this._proc) throw new Error("session not started");
		return this._proc.waitForMessage("debug");
	}

	stop(): void {
		if (this._proc) {
			this._proc.kill();
			this._proc = null;
		}
	}

	dispose(): void {
		this.stop();
		this._output.dispose();
		this._listeners.clear();
	}

	private _dispatch(msg: CliMessage): void {
		this._renderOutput(msg);

		for (const fn of this._listeners.get(msg.type) ?? []) fn(msg);
		for (const fn of this._listeners.get("*") ?? []) fn(msg);
	}

	private _renderOutput(msg: CliMessage): void {
		switch (msg.type) {
			case "info": {
				const p = msg.projection;
				this._writeOutput(p.name);
				if (p.source) this._writeOutput(`  Source: ${p.source}`);
				if (p.partitioning) this._writeOutput(`  Partitioning: ${p.partitioning}`);
				if (p.events) this._writeOutput(`  Events: ${p.events.join(", ")}`);
				if (p.engineVersion != null) this._writeOutput(`  Engine: v${p.engineVersion}`);
				this._writeOutput("");
				break;
			}
			case "event":
				this._writeOutput(`${msg.sequenceNumber}@${msg.streamId} ${msg.eventType}`);
				break;
			case "result":
				if (msg.status === "processed") {
					const partition = msg.partition ? ` [${msg.partition}]` : "";
					this._writeOutput(`  -> processed${partition}`);
					if (msg.logs?.length) {
						for (const l of msg.logs) this._writeOutput(`  [log] ${l}`);
					}
				} else {
					this._writeOutput(`  -> ${msg.status}: ${msg.reason}`);
				}
				break;
			case "error":
				this._writeOutput(`  ERROR: ${msg.code} - ${msg.description}`);
				break;
			case "summary":
				this._writeOutput("");
				this._writeOutput(
					`Summary: ${msg.handled} handled, ${msg.skipped} skipped, ${msg.errors} errors`,
				);
				break;
			case "debug":
			case "exit":
				break;
		}
	}

	private _writeOutput(text: string): void {
		this._output.appendLine(text);
	}
}
