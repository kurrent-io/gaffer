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

	it("drops file:// URL frames that resolve under a workspace folder", () => {
		// V8 emits `file:///abs/path/foo.js` for ESM frames; without
		// URL normalisation the path-prefix scrub would miss them.
		const out = buildException({
			err: makeError({
				stack: `    at userCode (file:///home/dev/proj/src/secret.js:7:1)
    at activate (${EXT_PATH}/dist/extension.js:42:13)`,
			}),
			phase: "event_processing",
			extensionPath: EXT_PATH,
			workspaceFolders: ["/home/dev/proj"],
		});
		const [entry] = out.properties.exceptions;
		expect(entry?.stacktrace.frames.map((f) => f.filename)).toEqual([
			"extension.js",
		]);
	});

	it("decodes percent-escapes in file:// URLs before the scrub", () => {
		// Uri.fsPath gives "/home/dev/My Proj"; V8 surfaces the same
		// path as "file:///home/dev/My%20Proj/..." in ESM stacks. The
		// fileURLToPath conversion must decode the escape so the
		// workspace-folder prefix check matches.
		const out = buildException({
			err: makeError({
				stack: `    at userCode (file:///home/dev/My%20Proj/src/secret.js:7:1)
    at activate (${EXT_PATH}/dist/extension.js:42:13)`,
			}),
			phase: "event_processing",
			extensionPath: EXT_PATH,
			workspaceFolders: ["/home/dev/My Proj"],
		});
		const [entry] = out.properties.exceptions;
		expect(entry?.stacktrace.frames.map((f) => f.filename)).toEqual([
			"extension.js",
		]);
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

describe("buildException message scrubbing", () => {
	function valueFrom(message: string, err?: unknown): string {
		const out = buildException({
			err:
				err ??
				makeError({
					message,
					stack: `    at f (${EXT_PATH}/dist/extension.js:1:1)`,
				}),
			phase: "event_processing",
			extensionPath: EXT_PATH,
			workspaceFolders: [],
		});
		return out.properties.exceptions[0]?.value ?? "";
	}

	it("scrubs a quoted POSIX absolute path from a Node fs error", () => {
		expect(
			valueFrom(
				"EACCES: permission denied, stat '/home/george/project/secret/gaffer.toml'",
			),
		).toBe("EACCES: permission denied, stat '<path>'");
	});

	it("scrubs a quoted path containing spaces in full (no tail leak)", () => {
		expect(
			valueFrom(
				"EACCES: permission denied, open '/Users/jane/My Project/gaffer.toml'",
			),
		).toBe("EACCES: permission denied, open '<path>'");
	});

	it("does not clip a URL scheme as a Windows drive path", () => {
		expect(valueFrom("request to https://eu.i.posthog.com/batch failed")).toBe(
			"request to https://eu.i.posthog.com/batch failed",
		);
	});

	it("scrubs a Windows drive-letter path", () => {
		expect(
			valueFrom(
				"EPERM: operation not permitted, open 'C:\\Users\\George\\AppData\\gaffer.toml'",
			),
		).toBe("EPERM: operation not permitted, open '<path>'");
	});

	it("scrubs a UNC path", () => {
		expect(
			valueFrom(
				String.raw`open failed for \\fileserver\share\secret\gaffer.toml`,
			),
		).toBe("open failed for <path>");
	});

	it("scrubs a file:// URL", () => {
		expect(
			valueFrom(
				"failed to import file:///home/george/My%20Proj/gaffer.toml at runtime",
			),
		).toBe("failed to import <path> at runtime");
	});

	it("scrubs a quoted file:// URL containing spaces in full", () => {
		expect(
			valueFrom("Cannot find module 'file:///home/jane/My Proj/x.js'"),
		).toBe("Cannot find module '<path>'");
	});

	it("scrubs a home-relative path", () => {
		expect(valueFrom("cannot open ~/projects/secret/gaffer.toml")).toBe(
			"cannot open <path>",
		);
	});

	it("strips an unquoted path only to the first space (documented best-effort)", () => {
		// Unquoted paths with spaces can't be bounded without eating trailing
		// prose, so the tail after the first space survives. The quoted rule
		// covers the common fs-error shape; Linux usernames can't contain
		// spaces and Node quotes paths in fs errors, so this is the residual.
		expect(valueFrom("see /tmp/My Cache/file for details")).toBe(
			"see <path> Cache/file for details",
		);
	});

	it("scrubs every path in a multi-path message", () => {
		expect(valueFrom("copy from '/a/b/c' to '/d/e/f' failed")).toBe(
			"copy from '<path>' to '<path>' failed",
		);
	});

	it("leaves an in-word slash alone (not a path)", () => {
		expect(valueFrom("merge conflict in read/write mode")).toBe(
			"merge conflict in read/write mode",
		);
	});

	it("leaves a message with no path untouched", () => {
		expect(
			valueFrom("Cannot read properties of undefined (reading 'foo')"),
		).toBe("Cannot read properties of undefined (reading 'foo')");
	});

	it("scrubs a path from a non-Error throwable", () => {
		expect(
			valueFrom("", "EACCES, open '/home/george/secret/gaffer.toml'"),
		).toBe("EACCES, open '<path>'");
	});

	it("redacts connection-string credentials and host, keeping the scheme and path", () => {
		expect(
			valueFrom(
				"connection failed: esdb://admin:changeit@cluster.kurrent.cloud:2113/db",
			),
		).toBe("connection failed: esdb://<redacted>/db");
	});

	it("redacts userinfo from an https URL", () => {
		expect(valueFrom("401 from https://user:pass@api.internal/v1")).toBe(
			"401 from https://<redacted>/v1",
		);
	});

	it("redacts a quoted connection string (incl. compound scheme)", () => {
		expect(
			valueFrom("bad dsn 'esdb+discover://admin:changeit@host:2113'"),
		).toBe("bad dsn 'esdb+discover://<redacted>'");
	});

	it("redacts user-only userinfo", () => {
		expect(valueFrom("auth required: redis://deploy@10.1.2.3:6379")).toBe(
			"auth required: redis://<redacted>",
		);
	});

	it("redacts a credential whose password contains a slash", () => {
		// The userinfo run stops at `@`, not the first `/`, so a base64 /
		// generated password with a `/` doesn't slip the redaction.
		expect(valueFrom("connect failed: esdb://admin:p/a/ss@host:2113/db")).toBe(
			"connect failed: esdb://<redacted>/db",
		);
	});

	it("keeps a multi-segment path after the redaction (later rules don't eat it)", () => {
		expect(valueFrom("esdb://u:p@host:2113/a/b/c down")).toBe(
			"esdb://<redacted>/a/b/c down",
		);
	});

	it("leaves a credential-less URL intact", () => {
		expect(valueFrom("request to https://gaffer.kurrent.io/docs failed")).toBe(
			"request to https://gaffer.kurrent.io/docs failed",
		);
	});

	it("does not treat a bare email or scp-style git remote as a connection string", () => {
		expect(valueFrom("contact admin@example.com for access")).toBe(
			"contact admin@example.com for access",
		);
		expect(valueFrom("clone failed: git@github.com:org/repo.git")).toBe(
			"clone failed: git@github.com:org/repo.git",
		);
	});
});
