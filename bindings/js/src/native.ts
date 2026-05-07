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

/** Result of a fallible call. errorJson is null on success; result may be
 * legitimately null (e.g. partition not seen) when errorJson is also null. */
export interface FallibleResult {
	result: string | null;
	errorJson: string | null;
}

export interface NativeBindings {
	sessionCreate(
		source: string,
		optionsJson: string | null,
	): { handle: number; errorJson: string | null };
	sessionDestroy(handle: number): void;
	knownBugs(): string;
	sessionFeed(handle: number, eventJson: string): FallibleResult;
	sessionGetState(handle: number, partition: string | null): string | null;
	sessionGetSharedState(handle: number): string | null;
	sessionSetState(
		handle: number,
		partition: string | null,
		stateJson: string,
	): void;
	sessionGetResult(handle: number, partition: string | null): FallibleResult;
	sessionGetSources(handle: number): FallibleResult;
	sessionGetPartitionKey(handle: number, eventJson: string): string | null;
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

	// gaffer_free must be bound first because the disposable string type
	// references it as the cleanup function.
	const gafferFree = l.func("gaffer_free", "void", ["void*"]);

	// Disposable string type: koffi reads the C string into a JS string and
	// then calls gafferFree on the original pointer. Used as the return type
	// for every fallible string-returning C export so we don't have to track
	// the raw pointer manually.
	const HeapStr = koffi.disposable("HeapStr", "str", gafferFree);

	// error_out is a `void**` slot the C function writes a `char*` into.
	// We allocate the slot per call via koffi.alloc("void*", 1) and pass it
	// as a `void*`. After the call, koffi.decode(slot, "char *") reads the
	// string at the slot's pointer; koffi.decode(slot, "void *") gives us
	// the External we pass to gafferFree.
	const errorSlotType = "void*";

	const sessionCreate = l.func("gaffer_session_create", "intptr", [
		"str",
		"str",
		errorSlotType,
	]);
	const sessionDestroy = l.func("gaffer_session_destroy", "void", ["intptr"]);
	const knownBugs = l.func("gaffer_known_bugs", HeapStr, []);
	const sessionFeed = l.func("gaffer_session_feed", HeapStr, [
		"intptr",
		"str",
		errorSlotType,
	]);
	const sessionGetState = l.func("gaffer_session_get_state", HeapStr, [
		"intptr",
		"str",
		errorSlotType,
	]);
	const sessionGetSharedState = l.func(
		"gaffer_session_get_shared_state",
		HeapStr,
		["intptr", errorSlotType],
	);
	const sessionSetState = l.func("gaffer_session_set_state", "void", [
		"intptr",
		"str",
		"str",
		errorSlotType,
	]);
	const sessionGetResult = l.func("gaffer_session_get_result", HeapStr, [
		"intptr",
		"str",
		errorSlotType,
	]);
	const sessionGetSources = l.func("gaffer_session_get_sources", HeapStr, [
		"intptr",
		errorSlotType,
	]);
	const sessionGetPartitionKey = l.func(
		"gaffer_session_get_partition_key",
		HeapStr,
		["intptr", "str", errorSlotType],
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
	const onStateChanged = l.func("gaffer_on_state_changed", "void", [
		"intptr",
		koffi.pointer(stateChangedCbType),
		"void*",
	]);

	// Reads the error pointer from a slot, decodes the string, and frees.
	// Returns null when the slot contains NULL (success path).
	function consumeErrorSlot(slot: unknown): string | null {
		const str = koffi.decode(slot, "char *") as string | null;
		if (str == null) return null;
		const ptr = koffi.decode(slot, "void *");
		gafferFree(ptr);
		return str;
	}

	function newErrorSlot(): unknown {
		return koffi.alloc("void*", 1);
	}

	bindings = {
		sessionCreate: (source, optionsJson) => {
			const errSlot = newErrorSlot();
			const handle = sessionCreate(source, optionsJson, errSlot) as number;
			return { handle, errorJson: consumeErrorSlot(errSlot) };
		},
		sessionDestroy: (handle) => sessionDestroy(handle),
		knownBugs: () => knownBugs() as string,
		sessionFeed: (handle, eventJson) => {
			const errSlot = newErrorSlot();
			const result = sessionFeed(handle, eventJson, errSlot) as string | null;
			return { result, errorJson: consumeErrorSlot(errSlot) };
		},
		sessionGetState: (handle, partition) => {
			const errSlot = newErrorSlot();
			const result = sessionGetState(handle, partition, errSlot) as
				| string
				| null;
			// silent: discard any error
			consumeErrorSlot(errSlot);
			return result;
		},
		sessionGetSharedState: (handle) => {
			const errSlot = newErrorSlot();
			const result = sessionGetSharedState(handle, errSlot) as string | null;
			consumeErrorSlot(errSlot);
			return result;
		},
		sessionSetState: (handle, partition, stateJson) => {
			const errSlot = newErrorSlot();
			sessionSetState(handle, partition, stateJson, errSlot);
			consumeErrorSlot(errSlot);
		},
		sessionGetResult: (handle, partition) => {
			const errSlot = newErrorSlot();
			const result = sessionGetResult(handle, partition, errSlot) as
				| string
				| null;
			return { result, errorJson: consumeErrorSlot(errSlot) };
		},
		sessionGetSources: (handle) => {
			const errSlot = newErrorSlot();
			const result = sessionGetSources(handle, errSlot) as string | null;
			return { result, errorJson: consumeErrorSlot(errSlot) };
		},
		sessionGetPartitionKey: (handle, eventJson) => {
			const errSlot = newErrorSlot();
			const result = sessionGetPartitionKey(handle, eventJson, errSlot) as
				| string
				| null;
			consumeErrorSlot(errSlot);
			return result;
		},
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
