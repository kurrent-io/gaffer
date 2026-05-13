import { describe, expect, it } from "vitest";

import { buildException } from "./exception.js";

const EXT_PATH = "/opt/gaffer/extension";

function makeError(args: {
	name?: string;
	message?: string;
	stack: string;
	cause?: unknown;
}): Error {
	const err = new Error(args.message ?? "boom");
	err.name = args.name ?? err.name;
	// Real V8 stack always starts with `Name: message`; we want the
	// frames we provide here to be the entire `at ...` block.
	err.stack = `${err.name}: ${err.message}\n${args.stack}`;
	if (args.cause !== undefined) {
		(err as Error & { cause?: unknown }).cause = args.cause;
	}
	return err;
}

describe("buildException", () => {
	it("emits a name=exception, phase=startup envelope from a plain Error", () => {
		const out = buildException({
			err: makeError({
				name: "TypeError",
				message: "x is not defined",
				stack: `    at activate (${EXT_PATH}/dist/extension.js:42:13)`,
			}),
			phase: "startup",
			extensionPath: EXT_PATH,
			workspaceFolders: [],
		});
		expect(out.name).toBe("exception");
		expect(out.properties.phase).toBe("startup");
		expect(out.properties.exceptions).toHaveLength(1);
		const [entry] = out.properties.exceptions;
		expect(entry?.type).toBe("TypeError");
		expect(entry?.value).toBe("x is not defined");
		expect(entry?.stacktrace.type).toBe("raw");
	});

	it("basenames gaffer-owned frames and marks them in_app", () => {
		const out = buildException({
			err: makeError({
				stack: `    at activate (${EXT_PATH}/dist/extension.js:42:13)
    at processTicksAndRejections (node:internal/process/task_queues:95:5)`,
			}),
			phase: "startup",
			extensionPath: EXT_PATH,
			workspaceFolders: [],
		});
		const [entry] = out.properties.exceptions;
		const frames = entry?.stacktrace.frames ?? [];
		expect(frames).toEqual([
			{
				filename: "extension.js",
				function: "activate",
				lineno: 42,
				in_app: true,
			},
			{
				filename: "task_queues",
				function: "processTicksAndRejections",
				lineno: 95,
				in_app: false,
			},
		]);
	});

	it("drops user-JS frames entirely (workspace path)", () => {
		const workspace = "/home/dev/my-project";
		const out = buildException({
			err: makeError({
				stack: `    at gafferHandler (${EXT_PATH}/dist/extension.js:7:1)
    at userCode (${workspace}/src/order.js:120:9)
    at gafferCallback (${EXT_PATH}/dist/extension.js:200:5)`,
			}),
			phase: "event_processing",
			extensionPath: EXT_PATH,
			workspaceFolders: [workspace],
		});
		const [entry] = out.properties.exceptions;
		const filenames = (entry?.stacktrace.frames ?? []).map((f) => f.filename);
		expect(filenames).toEqual(["extension.js", "extension.js"]);
		// User-code frames don't make in_app go true.
		expect(entry?.in_app).toBe(true);
	});

	it("parses anonymous-top-level frames (no function name)", () => {
		const out = buildException({
			err: makeError({
				stack: `    at ${EXT_PATH}/dist/extension.js:7:1`,
			}),
			phase: "startup",
			extensionPath: EXT_PATH,
			workspaceFolders: [],
		});
		const [entry] = out.properties.exceptions;
		expect(entry?.stacktrace.frames).toEqual([
			{ filename: "extension.js", lineno: 7, in_app: true },
		]);
	});

	it("skips lines that don't look like frames (e.g. `at <anonymous>`)", () => {
		const out = buildException({
			err: makeError({
				stack: `    at <anonymous>
    at activate (${EXT_PATH}/dist/extension.js:42:13)`,
			}),
			phase: "startup",
			extensionPath: EXT_PATH,
			workspaceFolders: [],
		});
		const [entry] = out.properties.exceptions;
		expect(entry?.stacktrace.frames).toHaveLength(1);
	});

	it("silently drops V8 eval frames (no usable filename)", () => {
		// V8 surfaces eval origins as nested `eval at ...` strings that
		// fall through both regexes. They have no usable filename, so
		// dropping is the right thing - assert one real frame survives.
		const out = buildException({
			err: makeError({
				stack: `    at eval (eval at <anonymous> (${EXT_PATH}/dist/extension.js:1:1), <anonymous>:1:1)
    at activate (${EXT_PATH}/dist/extension.js:42:13)`,
			}),
			phase: "startup",
			extensionPath: EXT_PATH,
			workspaceFolders: [],
		});
		const [entry] = out.properties.exceptions;
		expect(entry?.stacktrace.frames).toHaveLength(1);
		expect(entry?.stacktrace.frames[0]?.function).toBe("activate");
	});

	it("walks err.cause to build the causal chain (outer first, root last)", () => {
		const root = makeError({
			name: "ENOENT",
			message: "no such file",
			stack: `    at open (node:fs:1:1)`,
		});
		const middle = makeError({
			name: "ConfigError",
			message: "could not load config",
			stack: `    at loadConfig (${EXT_PATH}/dist/extension.js:7:1)`,
			cause: root,
		});
		const outer = makeError({
			name: "ActivationFailed",
			message: "activation failed",
			stack: `    at activate (${EXT_PATH}/dist/extension.js:42:13)`,
			cause: middle,
		});

		const out = buildException({
			err: outer,
			phase: "startup",
			extensionPath: EXT_PATH,
			workspaceFolders: [],
		});
		expect(out.properties.exceptions.map((e) => e.type)).toEqual([
			"ActivationFailed",
			"ConfigError",
			"ENOENT",
		]);
	});

	it("caps the causal chain at MAX_CAUSE_DEPTH (10) frames", () => {
		// Build a 20-deep chain; expect it to truncate to exactly 10.
		let cur: Error = makeError({
			stack: `    at root (${EXT_PATH}/dist/extension.js:1:1)`,
		});
		for (let i = 0; i < 19; i++) {
			cur = makeError({
				stack: `    at wrap${i} (${EXT_PATH}/dist/extension.js:${i}:1)`,
				cause: cur,
			});
		}
		const out = buildException({
			err: cur,
			phase: "startup",
			extensionPath: EXT_PATH,
			workspaceFolders: [],
		});
		expect(out.properties.exceptions.length).toBe(10);
	});

	it("classifies frames with Windows-style backslash paths", () => {
		const extWin = "C:\\Program Files\\gaffer";
		const wsWin = "C:\\Users\\dev\\my-project";
		const out = buildException({
			err: makeError({
				stack: `    at activate (${extWin}\\dist\\extension.js:42:13)
    at userCode (${wsWin}\\src\\order.js:120:9)
    at lib (C:\\system\\lib.js:1:1)`,
			}),
			phase: "startup",
			extensionPath: extWin,
			workspaceFolders: [wsWin],
		});
		const [entry] = out.properties.exceptions;
		expect(entry?.stacktrace.frames).toEqual([
			{
				filename: "extension.js",
				function: "activate",
				lineno: 42,
				in_app: true,
			},
			// userCode frame dropped (under wsWin).
			{ filename: "lib.js", function: "lib", lineno: 1, in_app: false },
		]);
	});

	it("doesn't mark in_app for a sibling path that shares a prefix with extensionPath", () => {
		// /opt/gaffer as extensionPath must NOT match /opt/gaffer-old/...
		const out = buildException({
			err: makeError({
				stack: `    at fn (/opt/gaffer-old/dist/extension.js:1:1)`,
			}),
			phase: "startup",
			extensionPath: "/opt/gaffer",
			workspaceFolders: [],
		});
		const [entry] = out.properties.exceptions;
		expect(entry?.stacktrace.frames[0]?.in_app).toBe(false);
	});

	it("doesn't drop a sibling directory that shares a prefix with a workspace folder", () => {
		// /home/dev as the workspace must NOT match /home/development-project/...
		// Plain startsWith would incorrectly drop the second frame; the
		// boundary-aware isUnderDir keeps it.
		const out = buildException({
			err: makeError({
				stack: `    at dev (/home/dev/src/order.js:1:1)
    at devbig (/home/development-project/src/lib.js:1:1)`,
			}),
			phase: "startup",
			extensionPath: EXT_PATH,
			workspaceFolders: ["/home/dev"],
		});
		const [entry] = out.properties.exceptions;
		expect(entry?.stacktrace.frames.map((f) => f.filename)).toEqual(["lib.js"]);
	});

	it("handles non-Error throwables (string, number) without crashing", () => {
		const out = buildException({
			err: "string-throw",
			phase: "startup",
			extensionPath: EXT_PATH,
			workspaceFolders: [],
		});
		expect(out.properties.exceptions).toHaveLength(1);
		expect(out.properties.exceptions[0]?.type).toBe("Error");
		expect(out.properties.exceptions[0]?.value).toBe("string-throw");
		expect(out.properties.exceptions[0]?.stacktrace.frames).toEqual([]);
	});

	it("sets in_app=false on the entry when all surviving frames are non-app", () => {
		const out = buildException({
			err: makeError({
				stack: `    at noop (node:internal/process/task_queues:1:1)`,
			}),
			phase: "startup",
			extensionPath: EXT_PATH,
			workspaceFolders: [],
		});
		expect(out.properties.exceptions[0]?.in_app).toBe(false);
	});
});
