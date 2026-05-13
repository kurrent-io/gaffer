import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
	buildGafferArgv,
	hasCommand,
	hasFlag,
	tryFetchManifest,
} from "./cli.js";
import {
	setConfiguration,
	setTrusted,
} from "../../test/testutil/vscode-state.js";
import type { Manifest } from "./schemas.js";

describe("buildGafferArgv", () => {
	it("uses the User-scope value when set", () => {
		setConfiguration("gaffer", "command", { globalValue: ["my-gaffer"] });
		expect(buildGafferArgv(["dev"])).toEqual(["my-gaffer", "dev"]);
	});

	it("falls back to gaffer when User-scope is empty array", () => {
		setConfiguration("gaffer", "command", { globalValue: [] });
		expect(buildGafferArgv(["dev"])).toEqual(["gaffer", "dev"]);
	});

	it("falls back to gaffer when no value is set", () => {
		expect(buildGafferArgv(["dev"])).toEqual(["gaffer", "dev"]);
	});

	it("ignores workspace-scope override (defense against hostile workspaces)", () => {
		// setConfiguration only models User-scope (globalValue) +
		// defaultValue. A workspace-scope value would arrive via the
		// `inspect` result; in production buildGafferArgv reads
		// `inspected?.globalValue` only, so any non-globalValue is
		// ignored. With nothing set, we get the default.
		setConfiguration("gaffer", "command", {});
		expect(buildGafferArgv(["dev"])).toEqual(["gaffer", "dev"]);
	});

	it("inserts --invoker-id and --invoked-by before the subcommand args", () => {
		expect(
			buildGafferArgv(["lsp"], {
				invokerId: "8f2b1a4c-9e7d-4a3e-b5f2-7c8a9d4e1f02",
			}),
		).toEqual([
			"gaffer",
			"--invoker-id=8f2b1a4c-9e7d-4a3e-b5f2-7c8a9d4e1f02",
			"--invoked-by=vscode",
			"lsp",
		]);
	});

	it("inserts --invoked-via when invokedVia is set alongside invokerId", () => {
		expect(
			buildGafferArgv(["dev", "proj"], {
				invokerId: "id-1",
				invokedVia: "code_lens",
			}),
		).toEqual([
			"gaffer",
			"--invoker-id=id-1",
			"--invoked-by=vscode",
			"--invoked-via=code_lens",
			"dev",
			"proj",
		]);
	});

	it("omits all linkage flags when invokerId is null", () => {
		expect(
			buildGafferArgv(["manifest"], {
				invokerId: null,
				invokedVia: "mcp_provider",
			}),
		).toEqual(["gaffer", "manifest"]);
	});

	it("omits linkage flags when no invocation is supplied", () => {
		expect(buildGafferArgv(["lsp"])).toEqual(["gaffer", "lsp"]);
	});
});

describe("gafferSpawnEnv", () => {
	it("returns undefined when the extension is not opted out (child inherits parent env)", async () => {
		const { gafferSpawnEnv } = await import("./cli.js");
		expect(gafferSpawnEnv(false)).toBeUndefined();
	});

	it("injects GAFFER_TELEMETRY_OPTOUT=1 alongside process.env when opted out", async () => {
		const { gafferSpawnEnv } = await import("./cli.js");
		const env = gafferSpawnEnv(true);
		if (env === undefined) throw new Error("expected an env override");
		expect(env.GAFFER_TELEMETRY_OPTOUT).toBe("1");
		// process.env keys are still present so the child gets PATH etc.
		expect(env.PATH).toBe(process.env.PATH);
	});
});

describe("gafferMcpEnv", () => {
	it("returns empty when not opted out", async () => {
		const { gafferMcpEnv } = await import("./cli.js");
		expect(gafferMcpEnv(false)).toEqual({});
	});

	it("returns the opt-out override when opted out", async () => {
		const { gafferMcpEnv } = await import("./cli.js");
		expect(gafferMcpEnv(true)).toEqual({ GAFFER_TELEMETRY_OPTOUT: "1" });
	});
});

describe("hasCommand / hasFlag", () => {
	const m: Manifest = {
		version: "1.0.0",
		commands: {
			dev: { flags: ["debug", "json"] },
			run: {},
		},
	};
	it("hasCommand returns false for null manifest", () => {
		expect(hasCommand(null, "dev")).toBe(false);
	});
	it("hasCommand returns true for present command", () => {
		expect(hasCommand(m, "dev")).toBe(true);
	});
	it("hasFlag returns true for a present flag", () => {
		expect(hasFlag(m, "dev", "debug")).toBe(true);
	});
	it("hasFlag returns false for a missing flag or missing command", () => {
		expect(hasFlag(m, "dev", "missing")).toBe(false);
		expect(hasFlag(m, "missing", "debug")).toBe(false);
	});
});

describe("tryFetchManifest - trust gate", () => {
	it("returns null without invoking the CLI when workspace is untrusted", async () => {
		setTrusted(false);
		const onError = vi.fn();
		const result = await tryFetchManifest(
			undefined,
			{ invokerId: () => null, isOptedOut: () => false },
			onError,
		);
		expect(result).toBeNull();
		expect(onError).not.toHaveBeenCalled();
	});
});

describe("tryFetchManifest - happy / error paths", () => {
	let tmpRoot: string;

	beforeEach(() => {
		setTrusted(true);
		tmpRoot = fs.mkdtempSync(path.join(os.tmpdir(), "gaffer-cli-"));
	});

	afterEach(() => {
		fs.rmSync(tmpRoot, { recursive: true, force: true });
	});

	function writeStub(name: string, body: string): string {
		const full = path.join(tmpRoot, name);
		fs.writeFileSync(full, body);
		fs.chmodSync(full, 0o755);
		return full;
	}

	it("returns the parsed manifest from a stub binary", async () => {
		// node -e prints a valid manifest to stdout when invoked with `manifest`.
		const stub = writeStub(
			"gaffer",
			`#!/bin/sh
if [ "$1" = "manifest" ]; then
  echo '{"version":"1.0.0","commands":{"dev":{"flags":["debug"]}}}'
fi
`,
		);
		setConfiguration("gaffer", "command", { globalValue: [stub] });

		const m = await tryFetchManifest(undefined, {
			invokerId: () => null,
			isOptedOut: () => false,
		});
		expect(m?.version).toBe("1.0.0");
		expect(m?.commands.dev?.flags).toEqual(["debug"]);
	});

	it("calls onError and returns null when the binary doesn't exist", async () => {
		setConfiguration("gaffer", "command", {
			globalValue: [path.join(tmpRoot, "nope")],
		});
		const onError = vi.fn();
		const result = await tryFetchManifest(
			undefined,
			{ invokerId: () => null, isOptedOut: () => false },
			onError,
		);
		expect(result).toBeNull();
		expect(onError).toHaveBeenCalledTimes(1);
	});

	it("preserves err.code on the rejected error (classifyManifestError contract)", async () => {
		setConfiguration("gaffer", "command", {
			globalValue: [path.join(tmpRoot, "nope")],
		});
		let captured: unknown;
		await tryFetchManifest(
			undefined,
			{ invokerId: () => null, isOptedOut: () => false },
			(err) => {
				captured = err;
			},
		);
		expect(captured).toBeDefined();
		// ENOENT is the load-bearing classification for binary_not_found.
		expect((captured as { code?: string }).code).toBe("ENOENT");
	});

	it("calls onError and returns null when stdout is invalid JSON", async () => {
		const stub = writeStub("gaffer", `#!/bin/sh\necho 'not json'\n`);
		setConfiguration("gaffer", "command", { globalValue: [stub] });
		const onError = vi.fn();
		const result = await tryFetchManifest(
			undefined,
			{ invokerId: () => null, isOptedOut: () => false },
			onError,
		);
		expect(result).toBeNull();
		expect(onError).toHaveBeenCalledTimes(1);
	});

	it("calls onError and returns null when JSON fails schema validation", async () => {
		const stub = writeStub("gaffer", `#!/bin/sh\necho '{"oops": true}'\n`);
		setConfiguration("gaffer", "command", { globalValue: [stub] });
		const onError = vi.fn();
		const result = await tryFetchManifest(
			undefined,
			{ invokerId: () => null, isOptedOut: () => false },
			onError,
		);
		expect(result).toBeNull();
		expect(onError).toHaveBeenCalledTimes(1);
		const err = onError.mock.calls[0]?.[0];
		expect(String(err)).toMatch(/malformed manifest/);
	});

	it("does not throw when onError is omitted on a fetch failure", async () => {
		setConfiguration("gaffer", "command", {
			globalValue: [path.join(tmpRoot, "missing")],
		});
		await expect(
			tryFetchManifest(undefined, {
				invokerId: () => null,
				isOptedOut: () => false,
			}),
		).resolves.toBeNull();
	});

	it("forwards invokerId through to the spawned CLI argv", async () => {
		// Stub dumps its argv into a sibling file we can read back, then
		// emits a minimal valid manifest. Round-trip proves --invoker-id
		// is appended verbatim.
		const argvLog = path.join(tmpRoot, "argv.log");
		const stub = writeStub(
			"gaffer",
			`#!/bin/sh
echo "$@" > "${argvLog}"
echo '{"version":"1.0.0","commands":{}}'
`,
		);
		setConfiguration("gaffer", "command", { globalValue: [stub] });
		const m = await tryFetchManifest(undefined, {
			invokerId: () => "abc-id",
			isOptedOut: () => false,
		});
		expect(m?.version).toBe("1.0.0");
		const args = fs.readFileSync(argvLog, "utf8").trim().split(" ");
		expect(args).toEqual([
			"--invoker-id=abc-id",
			"--invoked-by=vscode",
			"manifest",
		]);
	});
});
