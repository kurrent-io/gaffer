import koffi, { type IKoffiRegisteredCallback } from "koffi";
import { existsSync } from "node:fs";
import { resolve } from "node:path";
import { createRequire } from "node:module";

interface PlatformInfo {
	/** npm package that ships the native binary */
	packageName: string;
	/** binary filename inside the npm package (and at the dev publish path) */
	libFile: string;
	/** .NET runtime identifier used for the dev-fallback publish path */
	dotnetRid: string;
}

const PLATFORMS: Record<string, PlatformInfo> = {
	"linux-x64": {
		packageName: "@kurrent/gaffer-runtime-linux-x64",
		libFile: "gaffer.so",
		dotnetRid: "linux-x64",
	},
	"linux-arm64": {
		packageName: "@kurrent/gaffer-runtime-linux-arm64",
		libFile: "gaffer.so",
		dotnetRid: "linux-arm64",
	},
	"darwin-x64": {
		packageName: "@kurrent/gaffer-runtime-darwin-x64",
		libFile: "gaffer.dylib",
		dotnetRid: "osx-x64",
	},
	"darwin-arm64": {
		packageName: "@kurrent/gaffer-runtime-darwin-arm64",
		libFile: "gaffer.dylib",
		dotnetRid: "osx-arm64",
	},
	"win32-x64": {
		packageName: "@kurrent/gaffer-runtime-win32-x64",
		libFile: "gaffer.dll",
		dotnetRid: "win-x64",
	},
};

function findLibPath(): string {
	// 1. Explicit env var
	if (process.env.GAFFER_RUNTIME_LIB) {
		return process.env.GAFFER_RUNTIME_LIB;
	}

	const platformKey = `${process.platform}-${process.arch}`;
	const platform = PLATFORMS[platformKey];

	if (!platform) {
		throw new Error(
			`Unsupported platform ${platformKey}. ` +
				`Supported: ${Object.keys(PLATFORMS).join(", ")}.`,
		);
	}

	// 2. Platform-specific npm package
	try {
		const require = createRequire(import.meta.url);
		return require.resolve(`${platform.packageName}/${platform.libFile}`);
	} catch {
		// package or binary not found, fall through
	}

	// 3. Walk up looking for the runtime library (dev fallback)
	const dotnetLibFile = `Gaffer.Runtime.${platform.libFile.split(".").pop()}`;
	let dir = import.meta.dirname;
	for (let i = 0; i < 10; i++) {
		const candidate = resolve(
			dir,
			"runtime",
			"Gaffer.Runtime",
			"bin",
			"Release",
			"net10.0",
			platform.dotnetRid,
			"publish",
			dotnetLibFile,
		);
		if (existsSync(candidate)) return candidate;
		dir = resolve(dir, "..");
	}

	throw new Error(
		`Could not find Gaffer runtime library for ${platformKey}. ` +
			`Install ${platform.packageName} or set GAFFER_RUNTIME_LIB.`,
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
const stateChangedCbType = koffi.proto(
	"void gaffer_state_changed_cb(const char*, const char*, void*)",
);

export interface NativeBindings {
	sessionCreate(source: string, optionsJson: string | null): number;
	sessionDestroy(handle: number): void;
	sessionFeed(handle: number, eventJson: string): string | null;
	sessionGetState(handle: number, partition: string | null): string | null;
	sessionGetSharedState(handle: number): string | null;
	sessionSetState(
		handle: number,
		partition: string | null,
		stateJson: string,
	): void;
	sessionGetResult(handle: number, partition: string | null): string | null;
	sessionGetSources(handle: number): string | null;
	sessionGetPartitionKey(handle: number, eventJson: string): string | null;
	getLastError(): string | null;
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
	]);
	const sessionDestroy = l.func("gaffer_session_destroy", "void", ["intptr"]);
	const sessionFeed = l.func("gaffer_session_feed", "str", ["intptr", "str"]);
	const sessionGetState = l.func("gaffer_session_get_state", "str", [
		"intptr",
		"str",
	]);
	const sessionGetSharedState = l.func(
		"gaffer_session_get_shared_state",
		"str",
		["intptr"],
	);
	const sessionSetState = l.func("gaffer_session_set_state", "void", [
		"intptr",
		"str",
		"str",
	]);
	const sessionGetResult = l.func("gaffer_session_get_result", "str", [
		"intptr",
		"str",
	]);
	const sessionGetSources = l.func("gaffer_session_get_sources", "str", [
		"intptr",
	]);
	const sessionGetPartitionKey = l.func(
		"gaffer_session_get_partition_key",
		"str",
		["intptr", "str"],
	);
	const getLastError = l.func("gaffer_get_last_error", "str", []);
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
	const onStateChanged = l.func("gaffer_on_state_changed", "void", [
		"intptr",
		koffi.pointer(stateChangedCbType),
		"void*",
	]);

	bindings = {
		sessionCreate: (source, optionsJson) =>
			sessionCreate(source, optionsJson) as number,
		sessionDestroy: (handle) => sessionDestroy(handle),
		sessionFeed: (handle, eventJson) =>
			sessionFeed(handle, eventJson) as string | null,
		sessionGetState: (handle, partition) =>
			sessionGetState(handle, partition) as string | null,
		sessionGetSharedState: (handle) =>
			sessionGetSharedState(handle) as string | null,
		sessionSetState: (handle, partition, stateJson) =>
			sessionSetState(handle, partition, stateJson),
		sessionGetResult: (handle, partition) =>
			sessionGetResult(handle, partition) as string | null,
		sessionGetSources: (handle) => sessionGetSources(handle) as string | null,
		sessionGetPartitionKey: (handle, eventJson) =>
			sessionGetPartitionKey(handle, eventJson) as string | null,
		getLastError: () => getLastError() as string | null,
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
