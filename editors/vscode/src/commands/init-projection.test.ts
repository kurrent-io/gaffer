import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import * as vscode from "vscode";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { initProjection } from "./init-projection.js";
import {
	getShownMessages,
	getState,
	queueMessageResponse,
	resetVscode,
	setConfiguration,
	setTrusted,
	setWorkspaceFolders,
} from "../../test/testutil/vscode-state.js";

function makeFolder(name: string, fsPath: string): vscode.WorkspaceFolder {
	return { uri: vscode.Uri.file(fsPath), name, index: 0 };
}

const stubTelemetry = {
	invokerId: () => null,
	isOptedOut: () => false,
};

describe("initProjection - bail-early paths", () => {
	beforeEach(() => {
		resetVscode();
		setTrusted(true);
	});

	it("warns and returns when the workspace is untrusted", async () => {
		setTrusted(false);
		setWorkspaceFolders([makeFolder("proj", "/ws/proj")]);
		await initProjection({ telemetry: stubTelemetry })();
		const messages = getShownMessages();
		expect(messages).toHaveLength(1);
		expect(messages[0]?.kind).toBe("warning");
		expect(messages[0]?.message).toMatch(/trust this workspace/i);
		// Never reached the CLI spawn.
		expect(getState().executeCommandCalls).toEqual([]);
	});

	it("warns and returns when no workspace is open", async () => {
		setWorkspaceFolders([]);
		await initProjection({ telemetry: stubTelemetry })();
		const messages = getShownMessages();
		expect(messages).toHaveLength(1);
		expect(messages[0]?.kind).toBe("warning");
		expect(messages[0]?.message).toMatch(/open a folder first/i);
	});
});

describe("initProjection - toml-exists handling", () => {
	let tmpRoot: string;

	beforeEach(() => {
		resetVscode();
		setTrusted(true);
		tmpRoot = fs.mkdtempSync(path.join(os.tmpdir(), "gaffer-init-"));
		setWorkspaceFolders([makeFolder("proj", tmpRoot)]);
	});

	afterEach(() => {
		fs.rmSync(tmpRoot, { recursive: true, force: true });
	});

	it("offers Open existing when gaffer.toml is already present", async () => {
		fs.writeFileSync(path.join(tmpRoot, "gaffer.toml"), "engine_version = 2\n");
		queueMessageResponse("Open existing");
		await initProjection({ telemetry: stubTelemetry })();
		const messages = getShownMessages();
		expect(messages).toHaveLength(1);
		expect(messages[0]?.message).toMatch(/already exists/i);
		expect(messages[0]?.items).toContain("Open existing");
		const opened = getState().executeCommandCalls.find(
			(c) => c.name === "vscode.open",
		);
		expect(opened).toBeDefined();
		expect((opened?.args[0] as vscode.Uri).fsPath).toBe(
			path.join(tmpRoot, "gaffer.toml"),
		);
	});

	it("does not open the toml when the user dismisses the warning", async () => {
		fs.writeFileSync(path.join(tmpRoot, "gaffer.toml"), "engine_version = 2\n");
		// No queued response -> mock returns undefined (dismiss).
		await initProjection({ telemetry: stubTelemetry })();
		// Toast still fires - asserting this so a future change that
		// removes the warning entirely still trips a test.
		expect(getShownMessages()).toHaveLength(1);
		expect(
			getState().executeCommandCalls.find((c) => c.name === "vscode.open"),
		).toBeUndefined();
	});
});

// End-to-end with a real stub binary so we exercise the CLI spawn path
// (matches the discovery/cli.test.ts pattern). Tests both the happy
// path (toml gets created, opens) and a propagated failure.
describe("initProjection - CLI spawn", () => {
	let tmpRoot: string;
	let stubBin: string;

	beforeEach(() => {
		resetVscode();
		setTrusted(true);
		tmpRoot = fs.mkdtempSync(path.join(os.tmpdir(), "gaffer-init-spawn-"));
	});

	afterEach(() => {
		fs.rmSync(tmpRoot, { recursive: true, force: true });
	});

	function writeStub(body: string): string {
		stubBin = path.join(tmpRoot, "gaffer-stub");
		fs.writeFileSync(stubBin, body);
		fs.chmodSync(stubBin, 0o755);
		return stubBin;
	}

	it("opens the new toml after a successful init", async () => {
		const targetDir = path.join(tmpRoot, "ws");
		fs.mkdirSync(targetDir);
		setWorkspaceFolders([makeFolder("ws", targetDir)]);
		// Stub writes the toml ourselves so the post-spawn open path can
		// resolve it (real gaffer init does this) and dumps its argv to
		// a sibling file so the test can verify telemetry-linkage flags
		// reach the CLI.
		const argvLog = path.join(tmpRoot, "argv.log");
		const stub = writeStub(
			`#!/bin/sh
echo "$@" > "${argvLog}"
touch "${targetDir}/gaffer.toml"
echo "Initialized gaffer project"
`,
		);
		setConfiguration("gaffer", "command", { globalValue: [stub] });
		await initProjection({
			telemetry: { invokerId: () => "test-id", isOptedOut: () => false },
		})();
		const opened = getState().executeCommandCalls.find(
			(c) => c.name === "vscode.open",
		);
		expect(opened).toBeDefined();
		expect((opened?.args[0] as vscode.Uri).fsPath).toBe(
			path.join(targetDir, "gaffer.toml"),
		);
		const argv = fs.readFileSync(argvLog, "utf8").trim().split(" ");
		expect(argv).toEqual([
			"--invoker-id=test-id",
			"--invoked-by=vscode",
			"--invoked-via=command_palette",
			"init",
		]);
	});

	it("surfaces CLI stderr on init failure", async () => {
		const targetDir = path.join(tmpRoot, "ws2");
		fs.mkdirSync(targetDir);
		setWorkspaceFolders([makeFolder("ws2", targetDir)]);
		const stub = writeStub(
			`#!/bin/sh
echo "Error: boom" 1>&2
exit 1
`,
		);
		setConfiguration("gaffer", "command", { globalValue: [stub] });
		await initProjection({ telemetry: stubTelemetry })();
		const messages = getShownMessages();
		const err = messages.find((m) => m.kind === "error");
		expect(err).toBeDefined();
		expect(err?.message).toMatch(/gaffer init failed/i);
		expect(err?.message).toMatch(/Error: boom/);
	});
});
