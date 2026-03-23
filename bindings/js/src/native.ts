import koffi, { type IKoffiRegisteredCallback } from "koffi";
import { existsSync } from "fs";
import { resolve, join } from "path";
import { createRequire } from "module";

const PLATFORM_PACKAGES: Record<string, string> = {
	"linux-x64": "@kurrent/gaffer-runtime-linux-x64",
	// "darwin-arm64": "@kurrent/gaffer-runtime-darwin-arm64",
	// "darwin-x64": "@kurrent/gaffer-runtime-darwin-x64",
	// "win32-x64": "@kurrent/gaffer-runtime-win32-x64",
};

function findLibPath(): string {
	// 1. Explicit env var
	if (process.env.GAFFER_RUNTIME_LIB) {
		return process.env.GAFFER_RUNTIME_LIB;
	}

	// 2. Platform-specific npm package
	const platformKey = `${process.platform}-${process.arch}`;
	const packageName = PLATFORM_PACKAGES[platformKey];
	if (packageName) {
		try {
			const require = createRequire(import.meta.url);
			const packageDir = resolve(
				require.resolve(`${packageName}/package.json`),
				"..",
			);
			const candidate = join(packageDir, "gaffer.so");
			if (existsSync(candidate)) return candidate;
		} catch {
			// package not installed, fall through
		}
	}

	// 3. Walk up looking for the runtime .so (dev fallback)
	let dir = import.meta.dirname;
	for (let i = 0; i < 10; i++) {
		const candidate = resolve(
			dir,
			"runtime",
			"Gaffer.Runtime",
			"bin",
			"Release",
			"net10.0",
			"linux-x64",
			"publish",
			"Gaffer.Runtime.so",
		);
		if (existsSync(candidate)) return candidate;
		dir = resolve(dir, "..");
	}

	throw new Error(
		`Could not find Gaffer runtime library for ${platformKey}. ` +
			`Install @kurrent/gaffer-runtime-${platformKey} or set GAFFER_RUNTIME_LIB.`,
	);
}

let lib: koffi.IKoffiLib | null = null;

function getLib(): koffi.IKoffiLib {
	if (lib) return lib;
	lib = koffi.load(findLibPath());
	return lib;
}

// Callback types matching gaffer.h
const emitCbType = koffi.proto(
	"void gaffer_emit_cb(const char*, const char*, const char*, const char*, int, int, void*)",
);
const logCbType = koffi.proto("void gaffer_log_cb(const char*, void*)");
const slowHandlerCbType = koffi.proto(
	"void gaffer_slow_handler_cb(const char*, int, void*)",
);
const stateChangedCbType = koffi.proto(
	"void gaffer_state_changed_cb(const char*, const char*, void*)",
);

export const ERROR_BUF_SIZE = 4096;

export interface NativeBindings {
	sessionCreate(
		source: string,
		optionsJson: string | null,
		errorBuf: Buffer,
		errorBufSize: number,
	): number;
	sessionDestroy(handle: number): void;
	sessionFeed(
		handle: number,
		eventJson: string,
		errorBuf: Buffer,
		errorBufSize: number,
	): number;
	sessionGetState(
		handle: number,
		partition: string | null,
		errorBuf: Buffer,
		errorBufSize: number,
	): string | null;
	sessionGetSharedState(
		handle: number,
		errorBuf: Buffer,
		errorBufSize: number,
	): string | null;
	sessionSetState(
		handle: number,
		partition: string | null,
		stateJson: string,
	): void;
	sessionGetResult(
		handle: number,
		partition: string | null,
		errorBuf: Buffer,
		errorBufSize: number,
	): string | null;
	sessionGetSources(handle: number): string | null;
	sessionGetPartitionKey(
		handle: number,
		eventJson: string,
		errorBuf: Buffer,
		errorBufSize: number,
	): string | null;
	onEmit(
		handle: number,
		cb: (
			stream: string,
			type: string,
			data: string | null,
			metadata: string | null,
			isJson: boolean,
			isLink: boolean,
		) => void,
	): IKoffiRegisteredCallback;
	onLog(
		handle: number,
		cb: (message: string) => void,
	): IKoffiRegisteredCallback;
	onSlowHandler(
		handle: number,
		cb: (handler: string, durationMs: number) => void,
	): IKoffiRegisteredCallback;
	onStateChanged(
		handle: number,
		cb: (partition: string, state: string | null) => void,
	): IKoffiRegisteredCallback;
	unregisterCallback(cb: IKoffiRegisteredCallback): void;
}

let bindings: NativeBindings | null = null;

export function getNativeBindings(): NativeBindings {
	if (bindings) return bindings;

	const l = getLib();

	const sessionCreate = l.func("gaffer_session_create", "intptr", [
		"str",
		"str",
		"uint8_t*",
		"int",
	]);
	const sessionDestroy = l.func("gaffer_session_destroy", "void", ["intptr"]);
	const sessionFeed = l.func("gaffer_session_feed", "int", [
		"intptr",
		"str",
		"uint8_t*",
		"int",
	]);
	const sessionGetState = l.func("gaffer_session_get_state", "str", [
		"intptr",
		"str",
		"uint8_t*",
		"int",
	]);
	const sessionGetSharedState = l.func(
		"gaffer_session_get_shared_state",
		"str",
		["intptr", "uint8_t*", "int"],
	);
	const sessionSetState = l.func("gaffer_session_set_state", "void", [
		"intptr",
		"str",
		"str",
	]);
	const sessionGetResult = l.func("gaffer_session_get_result", "str", [
		"intptr",
		"str",
		"uint8_t*",
		"int",
	]);
	const sessionGetSources = l.func("gaffer_session_get_sources", "str", [
		"intptr",
	]);
	const sessionGetPartitionKey = l.func(
		"gaffer_session_get_partition_key",
		"str",
		["intptr", "str", "uint8_t*", "int"],
	);
	const onEmit = l.func("gaffer_on_emit", "void", [
		"intptr",
		koffi.pointer(emitCbType),
		"void*",
	]);
	const onLog = l.func("gaffer_on_log", "void", [
		"intptr",
		koffi.pointer(logCbType),
		"void*",
	]);
	const onSlowHandler = l.func("gaffer_on_slow_handler", "void", [
		"intptr",
		koffi.pointer(slowHandlerCbType),
		"void*",
	]);
	const onStateChanged = l.func("gaffer_on_state_changed", "void", [
		"intptr",
		koffi.pointer(stateChangedCbType),
		"void*",
	]);

	bindings = {
		sessionCreate: (source, optionsJson, errorBuf, errorBufSize) =>
			sessionCreate(source, optionsJson, errorBuf, errorBufSize) as number,
		sessionDestroy: (handle) => sessionDestroy(handle),
		sessionFeed: (handle, eventJson, errorBuf, errorBufSize) =>
			sessionFeed(handle, eventJson, errorBuf, errorBufSize) as number,
		sessionGetState: (handle, partition, errorBuf, errorBufSize) =>
			sessionGetState(handle, partition, errorBuf, errorBufSize) as
				| string
				| null,
		sessionGetSharedState: (handle, errorBuf, errorBufSize) =>
			sessionGetSharedState(handle, errorBuf, errorBufSize) as string | null,
		sessionSetState: (handle, partition, stateJson) =>
			sessionSetState(handle, partition, stateJson),
		sessionGetResult: (handle, partition, errorBuf, errorBufSize) =>
			sessionGetResult(handle, partition, errorBuf, errorBufSize) as
				| string
				| null,
		sessionGetSources: (handle) => sessionGetSources(handle) as string | null,
		sessionGetPartitionKey: (handle, eventJson, errorBuf, errorBufSize) =>
			sessionGetPartitionKey(handle, eventJson, errorBuf, errorBufSize) as
				| string
				| null,
		onEmit: (handle, cb) => {
			const nativeCb = koffi.register(
				(
					stream: string,
					type: string,
					data: string | null,
					metadata: string | null,
					isJson: number,
					isLink: number,
					_userData: unknown,
				) => {
					cb(stream, type, data, metadata, isJson !== 0, isLink !== 0);
				},
				koffi.pointer(emitCbType),
			);
			onEmit(handle, nativeCb, null);
			return nativeCb;
		},
		onLog: (handle, cb) => {
			const nativeCb = koffi.register((message: string, _userData: unknown) => {
				cb(message);
			}, koffi.pointer(logCbType));
			onLog(handle, nativeCb, null);
			return nativeCb;
		},
		onSlowHandler: (handle, cb) => {
			const nativeCb = koffi.register(
				(handler: string, durationMs: number, _userData: unknown) => {
					cb(handler, durationMs);
				},
				koffi.pointer(slowHandlerCbType),
			);
			onSlowHandler(handle, nativeCb, null);
			return nativeCb;
		},
		onStateChanged: (handle, cb) => {
			const nativeCb = koffi.register(
				(partition: string, state: string | null, _userData: unknown) => {
					cb(partition, state);
				},
				koffi.pointer(stateChangedCbType),
			);
			onStateChanged(handle, nativeCb, null);
			return nativeCb;
		},
		unregisterCallback: (cb) => {
			koffi.unregister(cb);
		},
	};

	return bindings;
}

export function readErrorBuf(buf: Buffer): string | null {
	const end = buf.indexOf(0);
	if (end <= 0) return null;
	return buf.toString("utf8", 0, end);
}
