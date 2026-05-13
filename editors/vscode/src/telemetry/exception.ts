// Build the `exception` event payload from an unknown error caught
// at one of the wrap sites in extension.ts. Walks `err.cause` to
// produce the causal chain (outer wrapper first, root cause last),
// parses V8 stack frames, scrubs filenames, drops user-JS frames.
//
// The wrappers (telemetry/wrap.ts) catch + emit + re-throw; this
// module is pure payload construction.

import type {
	Exception,
	ExceptionEntry,
	ExceptionPhase,
	Frame,
} from "@kurrent/gaffer-telemetry";

export type Phase = ExceptionPhase;

export interface BuildExceptionArgs {
	err: unknown;
	phase: Phase;
	/** Absolute path of the extension bundle root. Frames inside this
	 * tree are gaffer-owned (basenamed, in_app=true). */
	extensionPath: string;
	/** Workspace folder paths. Frames inside these are user code -
	 * dropped entirely so projection JS / project source never leaves
	 * the machine. */
	workspaceFolders: readonly string[];
}

/** Cap recursion in case someone hands us a cyclic cause chain. */
const MAX_CAUSE_DEPTH = 10;

export function buildException(args: BuildExceptionArgs): Exception {
	return {
		name: "exception",
		timestamp: new Date().toISOString(),
		properties: {
			exceptions: walkCauseChain(args.err).map((e) => makeEntry(e, args)),
			phase: args.phase,
		},
	};
}

function walkCauseChain(err: unknown): unknown[] {
	const chain: unknown[] = [];
	let cursor = err;
	for (let i = 0; i < MAX_CAUSE_DEPTH; i++) {
		chain.push(cursor);
		if (cursor instanceof Error && cursor.cause !== undefined) {
			cursor = cursor.cause;
			continue;
		}
		break;
	}
	return chain;
}

function makeEntry(
	err: unknown,
	args: { extensionPath: string; workspaceFolders: readonly string[] },
): ExceptionEntry {
	const type = err instanceof Error ? err.name : "Error";
	const value = err instanceof Error ? err.message : String(err);
	const stack = err instanceof Error ? (err.stack ?? "") : "";
	const frames = parseStack(stack)
		.map((f) => classifyFrame(f, args))
		.filter((f): f is Frame => f !== null);
	const inApp = frames.some((f) => f.in_app);
	return {
		type,
		value,
		in_app: inApp,
		stacktrace: {
			type: "raw",
			frames,
		},
	};
}

interface RawFrame {
	function?: string;
	filename: string;
	lineno?: number;
}

// V8 stack lines come in three shapes we care about:
//   "    at Foo.bar (/path/to/file.ts:42:13)"
//   "    at /path/to/file.ts:42:13"        (top-level)
//   "    at <anonymous>"                   (no source - skipped)
// Eval-frame shapes like "at eval (eval at <anonymous> (...), <anonymous>:1:1)"
// fall through both regexes and are silently dropped; rare and
// safe-to-drop (no usable filename anyway).
const FRAME_WITH_FUNC = /^\s*at\s+(.+?)\s+\((.+?):(\d+):\d+\)\s*$/;
const FRAME_NO_FUNC = /^\s*at\s+(.+?):(\d+):\d+\s*$/;

function parseStack(stack: string): RawFrame[] {
	const out: RawFrame[] = [];
	for (const line of stack.split("\n")) {
		const withFunc = FRAME_WITH_FUNC.exec(line);
		if (withFunc !== null) {
			const fn = withFunc[1];
			const file = withFunc[2];
			const ln = withFunc[3];
			if (fn === undefined || file === undefined || ln === undefined) continue;
			if (!isPathLike(file)) continue;
			out.push({ function: fn, filename: file, lineno: Number(ln) });
			continue;
		}
		const noFunc = FRAME_NO_FUNC.exec(line);
		if (noFunc !== null) {
			const file = noFunc[1];
			const ln = noFunc[2];
			if (file === undefined || ln === undefined) continue;
			if (!isPathLike(file)) continue;
			out.push({ filename: file, lineno: Number(ln) });
			continue;
		}
	}
	return out;
}

/** Eval-frame shapes ("eval at <anonymous> (foo:1:1), <anonymous>:1:1")
 * back-reference into the line until the regex finally matches with a
 * "filename" full of parens and angle brackets. Real source paths never
 * contain those, so reject them - the eval frame has no usable file to
 * basename anyway. */
function isPathLike(file: string): boolean {
	return !/[()<>]/.test(file);
}

function classifyFrame(
	raw: RawFrame,
	args: { extensionPath: string; workspaceFolders: readonly string[] },
): Frame | null {
	// User code: drop entirely. The projection file (or any workspace
	// JS the extension somehow ended up running) is the user's
	// content; we never put it on the wire.
	for (const folder of args.workspaceFolders) {
		if (folder !== "" && isUnderDir(raw.filename, folder)) return null;
	}
	const inApp = isUnderDir(raw.filename, args.extensionPath);
	const filename = basename(raw.filename);
	const frame: Frame = { filename, in_app: inApp };
	if (raw.function !== undefined) frame.function = raw.function;
	if (raw.lineno !== undefined) frame.lineno = raw.lineno;
	return frame;
}

/** True when `file` is the directory `dir` or any descendant. Plain
 * `startsWith` would let `/home/development` match a `/home/dev`
 * workspace, dropping frames that aren't user code. Boundary on
 * either separator works across posix and Windows-shaped paths. */
function isUnderDir(file: string, dir: string): boolean {
	if (!file.startsWith(dir)) return false;
	if (file.length === dir.length) return true;
	const next = file.charAt(dir.length);
	return next === "/" || next === "\\";
}

/** Cross-platform basename without pulling node:path - the input is
 * already a stack-frame filename, which can be a posix path, a
 * Windows path, or a `node:foo` builtin reference. */
function basename(filename: string): string {
	const slash = Math.max(filename.lastIndexOf("/"), filename.lastIndexOf("\\"));
	return slash === -1 ? filename : filename.slice(slash + 1);
}
