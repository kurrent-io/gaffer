// Build the `exception` event payload from an unknown error caught
// at one of the wrap sites in extension.ts. Walks `err.cause` to
// produce the causal chain (outer wrapper first, root cause last),
// parses V8 stack frames, scrubs filenames, drops user-JS frames.
//
// The wrappers (telemetry/wrap.ts) catch + emit + re-throw; this
// module is pure payload construction.

import { fileURLToPath } from "node:url";

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
	const value = scrubMessage(err instanceof Error ? err.message : String(err));
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

// Strip path- and credential-shaped substrings from an exception message.
// The schema contract for `value` is "only a message gaffer wrote", but Node
// / VS Code errors that bubble through the wrap sites embed verbatim (a)
// absolute filesystem paths - `EACCES: ... stat '/home/user/x'` - whose
// directories carry the username, and (b) connection-string credentials and
// private hostnames - `esdb://admin:changeit@cluster.kurrent.cloud:2113`.
// gaffer throws plain Errors with no brand to allowlist on, so we strip both
// and keep the diagnostic skeleton (`... stat '<path>'`, `esdb://<redacted>`).
//
// Scope: these wrap sites are extension-host surfaces (fs, child_process,
// vscode API), so paths and connection errors are the leak vectors.
// Projection / Jint messages - which the contract also forbids - originate
// in the CLI, not here, so this doesn't attempt general identifier removal.
//
// Rules run in order. The credential rule goes first: a URL with userinfo
// (`scheme://user:pass@host`) has its authority redacted, keeping the scheme
// (a non-sensitive signal it was a connection failure) and the path after it
// (unscrubbed - a db name is low-sensitivity and aids diagnosis). The userinfo
// span runs to the `@`, so a `/` in the password doesn't defeat it; the cost
// is that a credential-less URL with an `@` in its path is over-redacted,
// which leaks nothing. A URL without any `@` (gaffer's own `https://`
// endpoints) is left intact. The
// quoted rule handles the common fs-error shape `... '<abs path>'`: it spans
// the whole quoted run, so a path with spaces ("My Project") doesn't leak its
// tail, and it fires only when the quoted content opens with a path indicator
// (`file://` included) so a quoted identifier like 'foo' is left alone. The
// unquoted rules are the fallback; an unquoted path with spaces strips only
// to the first space. The POSIX and `~` rules are boundary-anchored so an
// in-word slash like "read/write" isn't mistaken for a path, and the Windows
// drive rule rejects a preceding letter so a URL scheme isn't clipped.
const SCRUB_RULES: ReadonlyArray<
	[RegExp, (match: string, group1: string) => string]
> = [
	[
		/(\b[a-z][a-z\d+.-]*:\/\/)[^\s'"`@]*@[^/\s'"`]*/gi,
		(_m, scheme) => `${scheme}<redacted>`,
	],
	[
		/(['"])(?:file:\/\/|\/|~\/|\\\\|[A-Za-z]:[\\/])[^'"]*\1/g,
		(_m, quote) => `${quote}<path>${quote}`,
	],
	[/file:\/\/[^\s'"`]*/gi, () => "<path>"],
	[/(?<![A-Za-z])[A-Za-z]:[\\/][^\s'"`<>|]*/g, () => "<path>"],
	[/\\\\[^\s'"`<>|]+/g, () => "<path>"],
	[/(?<=^|[\s'"`(:=,])~\/[^\s'"`<>]*/g, () => "<path>"],
	[/(?<=^|[\s'"`(:=,])(?:\/[^\s/'"`<>:*?|]+)+\/?/g, () => "<path>"],
];

function scrubMessage(message: string): string {
	let out = message;
	for (const [pattern, replacement] of SCRUB_RULES) {
		out = out.replace(pattern, replacement);
	}
	return out;
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
	// Normalise `file://` URL filenames (V8 emits these for ESM and
	// import.meta.url-derived call sites) so the path-prefix scrub
	// below catches them on either side of the URL/path divide.
	const file = normaliseFilename(raw.filename);
	// User code: drop entirely. The projection file (or any workspace
	// JS the extension somehow ended up running) is the user's
	// content; we never put it on the wire.
	for (const folder of args.workspaceFolders) {
		if (folder !== "" && isUnderDir(file, folder)) return null;
	}
	const inApp = isUnderDir(file, args.extensionPath);
	const filename = basename(file);
	const frame: Frame = { filename, in_app: inApp };
	if (raw.function !== undefined) frame.function = raw.function;
	if (raw.lineno !== undefined) frame.lineno = raw.lineno;
	return frame;
}

/** Convert `file:///home/user/foo.js` to `/home/user/foo.js`. Leaves
 * non-URL filenames (`node:internal/...`, `/abs/path`, `webpack:...`)
 * untouched. Without this the workspace-folder scrub would miss any
 * frame V8 emitted as a URL.
 *
 * Uses `fileURLToPath` rather than `URL.pathname` so percent-escapes
 * decode (e.g. `%20` -> ` `) and Windows URLs produce native paths
 * (`c:\...` not `/c:/...`). `Uri.fsPath` - the shape workspaceFolders
 * arrive in - has the same convention. */
function normaliseFilename(filename: string): string {
	if (filename.startsWith("file://")) {
		try {
			return fileURLToPath(filename);
		} catch {
			// Malformed URL - leave as-is rather than risk losing the
			// scrub on a string that happened to start with file://.
			return filename;
		}
	}
	return filename;
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
