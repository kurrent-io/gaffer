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

	it("falls back to ./bin/gaffer when User-scope is empty array", () => {
		setConfiguration("gaffer", "command", { globalValue: [] });
		expect(buildGafferArgv(["dev"])).toEqual(["./bin/gaffer", "dev"]);
	});

	it("falls back to ./bin/gaffer when no value is set", () => {
		expect(buildGafferArgv(["dev"])).toEqual(["./bin/gaffer", "dev"]);
	});

	it("ignores workspace-scope override (defense against hostile workspaces)", () => {
		// setConfiguration only models User-scope (globalValue) +
		// defaultValue. A workspace-scope value would arrive via the
		// `inspect` result; in production buildGafferArgv reads
		// `inspected?.globalValue` only, so any non-globalValue is
		// ignored. With nothing set, we get the default.
		setConfiguration("gaffer", "command", {});
		expect(buildGafferArgv(["dev"])).toEqual(["./bin/gaffer", "dev"]);
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
		const result = await tryFetchManifest(undefined, onError);
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

		const m = await tryFetchManifest(undefined);
		expect(m?.version).toBe("1.0.0");
		expect(m?.commands.dev?.flags).toEqual(["debug"]);
	});

	it("calls onError and returns null when the binary doesn't exist", async () => {
		setConfiguration("gaffer", "command", {
			globalValue: [path.join(tmpRoot, "nope")],
		});
		const onError = vi.fn();
		const result = await tryFetchManifest(undefined, onError);
		expect(result).toBeNull();
		expect(onError).toHaveBeenCalledTimes(1);
	});

	it("calls onError and returns null when stdout is invalid JSON", async () => {
		const stub = writeStub("gaffer", `#!/bin/sh\necho 'not json'\n`);
		setConfiguration("gaffer", "command", { globalValue: [stub] });
		const onError = vi.fn();
		const result = await tryFetchManifest(undefined, onError);
		expect(result).toBeNull();
		expect(onError).toHaveBeenCalledTimes(1);
	});

	it("calls onError and returns null when JSON fails schema validation", async () => {
		const stub = writeStub("gaffer", `#!/bin/sh\necho '{"oops": true}'\n`);
		setConfiguration("gaffer", "command", { globalValue: [stub] });
		const onError = vi.fn();
		const result = await tryFetchManifest(undefined, onError);
		expect(result).toBeNull();
		expect(onError).toHaveBeenCalledTimes(1);
		const err = onError.mock.calls[0]?.[0];
		expect(String(err)).toMatch(/malformed manifest/);
	});

	it("does not throw when onError is omitted on a fetch failure", async () => {
		setConfiguration("gaffer", "command", {
			globalValue: [path.join(tmpRoot, "missing")],
		});
		await expect(tryFetchManifest(undefined)).resolves.toBeNull();
	});
});
