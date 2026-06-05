import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import * as vscode from "vscode";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
	runScaffoldWizard,
	scaffoldProjection,
	validateName,
	validatePath,
	type ScaffoldChoices,
	type ScaffoldProjectionDeps,
	type StepResult,
	type WizardSteps,
} from "./scaffold-projection.js";
import {
	getShownMessages,
	getState,
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

function cannedWizard(
	choices: ScaffoldChoices | undefined,
): ScaffoldProjectionDeps["wizard"] {
	let pending: ScaffoldChoices | undefined = choices;
	return () => {
		const r = pending;
		pending = undefined;
		return Promise.resolve(r);
	};
}

function makeDeps(
	overrides: Partial<ScaffoldProjectionDeps> = {},
): ScaffoldProjectionDeps {
	return {
		telemetry: stubTelemetry,
		wizard: cannedWizard(sampleChoices),
		...overrides,
	};
}

const sampleChoices: ScaffoldChoices = {
	pathArg: "projections/order-totals.js",
	source: { kind: "category", name: "orders" },
	partition: "per-stream",
	emit: false,
};

describe("validateName", () => {
	it("accepts a plain name", () => {
		expect(validateName("counter")).toBeUndefined();
	});
	it("accepts dashes, underscores, dots in the middle", () => {
		expect(validateName("order-totals")).toBeUndefined();
		expect(validateName("v2.beta_run")).toBeUndefined();
	});
	it("rejects empty / whitespace", () => {
		expect(validateName("")).toMatch(/required/i);
		expect(validateName("   ")).toMatch(/required/i);
	});
	it("rejects path separators", () => {
		expect(validateName("foo/bar")).toMatch(/path separator/i);
		expect(validateName("foo\\bar")).toMatch(/path separator/i);
	});
	it("rejects directory traversal", () => {
		expect(validateName("..")).toMatch(/'\.\.'/);
		expect(validateName("foo..bar")).toMatch(/'\.\.'/);
	});
	it("rejects Windows drive-letter prefixes", () => {
		// LLMs and Windows users may type "C:foo"; without the explicit
		// check, the renderer appends `.js` and the CLI's drive-letter
		// reject is what surfaces, which is a worse error message.
		expect(validateName("C:foo")).toMatch(/drive-letter/i);
		expect(validateName("c:foo")).toMatch(/drive-letter/i);
	});
	it("rejects extension-only inputs", () => {
		// ".js" alone would survive the renderer's idempotent
		// .js-append and create a file named just ".js".
		expect(validateName(".js")).toMatch(/required/i);
	});
});

describe("validatePath", () => {
	it("accepts a plain .js path", () => {
		expect(validatePath("projections/counter.js")).toBeUndefined();
		expect(validatePath("counter.js")).toBeUndefined();
		expect(validatePath("lib/handlers/totals.js")).toBeUndefined();
	});
	it("rejects empty / whitespace", () => {
		expect(validatePath("")).toMatch(/required/i);
		expect(validatePath("   ")).toMatch(/required/i);
	});
	it("rejects paths without a .js extension", () => {
		expect(validatePath("counter")).toMatch(/must end in \.js/i);
		expect(validatePath("counter.go")).toMatch(/must end in \.js/i);
	});
	it("rejects parent traversal", () => {
		expect(validatePath("../foo.js")).toMatch(/outside the project root/i);
		expect(validatePath("sub/../../foo.js")).toMatch(
			/outside the project root/i,
		);
		// Slash-only normalisation: backslash-prefixed traversal too.
		expect(validatePath("..\\foo.js")).toMatch(/outside the project root/i);
	});
	it("rejects absolute paths", () => {
		expect(validatePath("/etc/foo.js")).toMatch(
			/relative to the project root/i,
		);
	});
	it("rejects Windows drive-letter paths", () => {
		expect(validatePath("C:\\tmp\\foo.js")).toMatch(/drive-letter/i);
		expect(validatePath("C:/tmp/foo.js")).toMatch(/drive-letter/i);
		expect(validatePath("c:foo.js")).toMatch(/drive-letter/i);
	});
	it("rejects extension-only paths", () => {
		expect(validatePath(".js")).toMatch(/missing a file name/i);
		expect(validatePath("foo/.js")).toMatch(/missing a file name/i);
	});
});

describe("scaffoldProjection - bail-early paths", () => {
	beforeEach(() => {
		resetVscode();
		setTrusted(true);
	});

	it("warns and returns when the workspace is untrusted", async () => {
		setTrusted(false);
		setWorkspaceFolders([makeFolder("proj", "/ws/proj")]);
		await scaffoldProjection(makeDeps())();
		expect(getShownMessages()[0]?.kind).toBe("warning");
		expect(getShownMessages()[0]?.message).toMatch(/trust this workspace/i);
		expect(getState().executeCommandCalls).toEqual([]);
	});

	it("warns and returns when no workspace is open", async () => {
		setWorkspaceFolders([]);
		await scaffoldProjection(makeDeps())();
		expect(getShownMessages()[0]?.message).toMatch(/open a folder first/i);
	});
});

describe("scaffoldProjection - auto-init when no gaffer.toml", () => {
	let tmpRoot: string;

	beforeEach(() => {
		resetVscode();
		setTrusted(true);
		tmpRoot = fs.mkdtempSync(path.join(os.tmpdir(), "gaffer-scaffold-"));
		setWorkspaceFolders([makeFolder("proj", tmpRoot)]);
	});

	afterEach(() => {
		fs.rmSync(tmpRoot, { recursive: true, force: true });
	});

	it("spawns `gaffer init --yes` at the workspace root before scaffolding when no toml exists", async () => {
		// Stub records each invocation's argv + cwd to a separate
		// file so the test can assert init ran before scaffold.
		const initArgv = path.join(tmpRoot, "init.argv");
		const initCwd = path.join(tmpRoot, "init.cwd");
		const scaffoldArgv = path.join(tmpRoot, "scaffold.argv");
		const stub = path.join(tmpRoot, "gaffer-stub");
		fs.writeFileSync(
			stub,
			`#!/bin/sh
for a in "$@"; do
  if [ "$a" = "init" ]; then
    echo "$@" > "${initArgv}"
    pwd > "${initCwd}"
    touch "${tmpRoot}/gaffer.toml"
    exit 0
  fi
  if [ "$a" = "scaffold" ]; then
    echo "$@" > "${scaffoldArgv}"
    mkdir -p "${tmpRoot}/projections"
    touch "${tmpRoot}/projections/order-totals.js"
    exit 0
  fi
done
exit 1
`,
		);
		fs.chmodSync(stub, 0o755);
		setConfiguration("gaffer", "command", { globalValue: [stub] });
		await scaffoldProjection(makeDeps())();
		expect(fs.existsSync(initArgv)).toBe(true);
		expect(fs.readFileSync(initArgv, "utf8")).toMatch(/init --yes/);
		expect(fs.readFileSync(initCwd, "utf8").trim()).toBe(tmpRoot);
		expect(fs.existsSync(scaffoldArgv)).toBe(true);
		// Visible trace so the new gaffer.toml doesn't appear unannounced.
		const info = getShownMessages().find((m) => m.kind === "info");
		expect(info?.message).toMatch(/initialized gaffer project/i);
	});

	it("warns when the clicked URI is outside any workspace folder", async () => {
		// VS Code can fire the explorer-context menu on a folder that
		// isn't part of any registered workspace folder. Without the
		// guard, getWorkspaceFolder returns undefined and the init
		// path falls into the wrong message.
		const outside = fs.mkdtempSync(
			path.join(os.tmpdir(), "gaffer-scaffold-outside-"),
		);
		try {
			// No workspace folder containing `outside`. Mock's
			// getWorkspaceFolder still returns the first registered
			// folder; simulate "no match" by clearing folders.
			setWorkspaceFolders([]);
			await scaffoldProjection(makeDeps())(vscode.Uri.file(outside));
			const warn = getShownMessages().find((m) => m.kind === "warning");
			expect(warn?.message).toMatch(/isn't part of an open workspace/i);
		} finally {
			fs.rmSync(outside, { recursive: true, force: true });
		}
	});

	it("skips init when a gaffer.toml already exists", async () => {
		fs.writeFileSync(path.join(tmpRoot, "gaffer.toml"), "engine_version = 2\n");
		const initArgv = path.join(tmpRoot, "init.argv");
		const stub = path.join(tmpRoot, "gaffer-stub");
		fs.writeFileSync(
			stub,
			`#!/bin/sh
if [ "$1" = "init" ]; then echo "$@" > "${initArgv}"; fi
mkdir -p "${tmpRoot}/projections"
touch "${tmpRoot}/projections/order-totals.js"
`,
		);
		fs.chmodSync(stub, 0o755);
		setConfiguration("gaffer", "command", { globalValue: [stub] });
		await scaffoldProjection(makeDeps())();
		expect(fs.existsSync(initArgv)).toBe(false);
	});

	it("surfaces an error and stops if init fails", async () => {
		const stub = path.join(tmpRoot, "gaffer-stub");
		fs.writeFileSync(
			stub,
			`#!/bin/sh
if [ "$1" = "init" ]; then
  echo "Error: init blew up" 1>&2
  exit 1
fi
exit 0
`,
		);
		fs.chmodSync(stub, 0o755);
		setConfiguration("gaffer", "command", { globalValue: [stub] });
		await scaffoldProjection(makeDeps())();
		const err = getShownMessages().find((m) => m.kind === "error");
		expect(err?.message).toMatch(/gaffer init failed/i);
		expect(err?.message).toMatch(/init blew up/);
	});
});

describe("scaffoldProjection - explorer-context URI", () => {
	let tmpRoot: string;

	beforeEach(() => {
		resetVscode();
		setTrusted(true);
		tmpRoot = fs.mkdtempSync(path.join(os.tmpdir(), "gaffer-scaffold-ctx-"));
	});

	afterEach(() => {
		fs.rmSync(tmpRoot, { recursive: true, force: true });
	});

	it("uses the clicked folder URI as the target, bypassing the multi-root picker", async () => {
		// Two workspace folders, but the clicked URI points at a third
		// path (mirrors a user right-clicking a subfolder). The picker
		// must never appear.
		setWorkspaceFolders([makeFolder("a", "/ws/a"), makeFolder("b", "/ws/b")]);
		fs.writeFileSync(path.join(tmpRoot, "gaffer.toml"), "engine_version = 2\n");
		const stub = path.join(tmpRoot, "gaffer-stub");
		fs.writeFileSync(
			stub,
			`#!/bin/sh\ntouch "${tmpRoot}/projections/order-totals.js" 2>/dev/null || mkdir -p "${tmpRoot}/projections" && touch "${tmpRoot}/projections/order-totals.js"\n`,
		);
		fs.chmodSync(stub, 0o755);
		setConfiguration("gaffer", "command", { globalValue: [stub] });
		await scaffoldProjection(makeDeps())(vscode.Uri.file(tmpRoot));
		expect(getState().quickPickCalls).toEqual([]);
	});

	it("spawns at the clicked folder with a bare <name>.js path arg", async () => {
		// Explorer-context invocation: file should land in the clicked
		// folder (not the conventional projections/ subdirectory) and
		// the CLI cwd should be the clicked folder so its FindRoot
		// walks up from there.
		setWorkspaceFolders([makeFolder("ws", tmpRoot)]);
		fs.writeFileSync(path.join(tmpRoot, "gaffer.toml"), "engine_version = 2\n");
		const nested = path.join(tmpRoot, "my", "great", "projections");
		fs.mkdirSync(nested, { recursive: true });
		const argvLog = path.join(tmpRoot, "argv.log");
		const cwdLog = path.join(tmpRoot, "cwd.log");
		const stub = path.join(tmpRoot, "gaffer-stub");
		fs.writeFileSync(
			stub,
			`#!/bin/sh
echo "$@" > "${argvLog}"
pwd > "${cwdLog}"
touch "${nested}/order-totals.js"
`,
		);
		fs.chmodSync(stub, 0o755);
		setConfiguration("gaffer", "command", { globalValue: [stub] });
		// Explorer-flavor wizard would return just "<name>.js" (the
		// renderer appends .js to the typed name).
		await scaffoldProjection(
			makeDeps({
				wizard: cannedWizard({ ...sampleChoices, pathArg: "order-totals.js" }),
			}),
		)(vscode.Uri.file(nested));
		// cwd is the clicked folder; the CLI's FindRoot then walks up
		// to the workspace root on its own.
		expect(fs.readFileSync(cwdLog, "utf8").trim()).toBe(nested);
		// Path arg is the bare basename - the file lands where clicked.
		const argv = fs.readFileSync(argvLog, "utf8").trim().split(" ");
		expect(argv).toContain("order-totals.js");
		expect(argv).not.toContain("projections/order-totals.js");
		const opened = getState().executeCommandCalls.find(
			(c) => c.name === "vscode.open",
		);
		expect((opened?.args[0] as vscode.Uri).fsPath).toBe(
			path.join(nested, "order-totals.js"),
		);
	});
});

describe("scaffoldProjection - CLI spawn", () => {
	let tmpRoot: string;

	beforeEach(() => {
		resetVscode();
		setTrusted(true);
		tmpRoot = fs.mkdtempSync(path.join(os.tmpdir(), "gaffer-scaffold-spawn-"));
	});

	afterEach(() => {
		fs.rmSync(tmpRoot, { recursive: true, force: true });
	});

	function writeStub(body: string): string {
		const stub = path.join(tmpRoot, "gaffer-stub");
		fs.writeFileSync(stub, body);
		fs.chmodSync(stub, 0o755);
		return stub;
	}

	it("invokes the CLI with the wizard's choices and opens the new .js", async () => {
		fs.writeFileSync(path.join(tmpRoot, "gaffer.toml"), "engine_version = 2\n");
		setWorkspaceFolders([makeFolder("proj", tmpRoot)]);
		const argvLog = path.join(tmpRoot, "argv.log");
		const stub = writeStub(
			`#!/bin/sh
echo "$@" > "${argvLog}"
mkdir -p "${tmpRoot}/projections"
touch "${tmpRoot}/projections/order-totals.js"
echo "Created projections/order-totals.js"
`,
		);
		setConfiguration("gaffer", "command", { globalValue: [stub] });
		await scaffoldProjection(
			makeDeps({
				telemetry: { invokerId: () => "test-id", isOptedOut: () => false },
			}),
		)();
		const argv = fs.readFileSync(argvLog, "utf8").trim().split(" ");
		expect(argv).toEqual([
			"--invoker-id=test-id",
			"--invoked-by=vscode",
			"--invoked-via=command_palette",
			"scaffold",
			"projections/order-totals.js",
			"--source=category:orders",
			"--partition=per-stream",
		]);
		const opened = getState().executeCommandCalls.find(
			(c) => c.name === "vscode.open",
		);
		expect((opened?.args[0] as vscode.Uri).fsPath).toBe(
			path.join(tmpRoot, "projections", "order-totals.js"),
		);
	});

	it("appends --emit only when the user picked it", async () => {
		fs.writeFileSync(path.join(tmpRoot, "gaffer.toml"), "engine_version = 2\n");
		setWorkspaceFolders([makeFolder("proj", tmpRoot)]);
		const argvLog = path.join(tmpRoot, "argv.log");
		const stub = writeStub(
			`#!/bin/sh
echo "$@" > "${argvLog}"
mkdir -p "${tmpRoot}/projections"
touch "${tmpRoot}/projections/p.js"
`,
		);
		setConfiguration("gaffer", "command", { globalValue: [stub] });
		await scaffoldProjection(
			makeDeps({
				wizard: cannedWizard({
					pathArg: "projections/p.js",
					source: { kind: "all" },
					partition: "none",
					emit: true,
				}),
			}),
		)();
		const argv = fs.readFileSync(argvLog, "utf8").trim().split(" ");
		expect(argv).toContain("--source=all");
		expect(argv).toContain("--partition=none");
		expect(argv).toContain("--emit");
	});

	it("surfaces CLI stderr on scaffold failure", async () => {
		fs.writeFileSync(path.join(tmpRoot, "gaffer.toml"), "engine_version = 2\n");
		setWorkspaceFolders([makeFolder("proj", tmpRoot)]);
		const stub = writeStub(
			`#!/bin/sh
echo "Error: name already taken" 1>&2
exit 1
`,
		);
		setConfiguration("gaffer", "command", { globalValue: [stub] });
		await scaffoldProjection(makeDeps())();
		const err = getShownMessages().find((m) => m.kind === "error");
		expect(err?.message).toMatch(/gaffer scaffold failed/i);
		expect(err?.message).toMatch(/name already taken/);
	});

	it("does nothing when the wizard is cancelled", async () => {
		fs.writeFileSync(path.join(tmpRoot, "gaffer.toml"), "engine_version = 2\n");
		setWorkspaceFolders([makeFolder("proj", tmpRoot)]);
		const stub = writeStub(`#!/bin/sh\necho "should-not-run" 1>&2\nexit 99\n`);
		setConfiguration("gaffer", "command", { globalValue: [stub] });
		await scaffoldProjection(makeDeps({ wizard: cannedWizard(undefined) }))();
		expect(getShownMessages()).toEqual([]);
	});
});

// ---- runScaffoldWizard with stub renderers ------------------------------

// Build a WizardSteps where each renderer returns a canned StepResult
// pulled from a queue. Lets tests drive the state machine through
// arbitrary paths (happy / cancel / back) without touching the
// QuickInput API.
function queuedSteps(): {
	steps: WizardSteps;
	push: <K extends keyof WizardSteps>(
		key: K,
		result: StepResult<unknown>,
	) => void;
	calls: { step: keyof WizardSteps; prev: unknown }[];
} {
	const queues: { [K in keyof WizardSteps]: StepResult<unknown>[] } = {
		pathArg: [],
		source: [],
		sourceName: [],
		partition: [],
		emit: [],
	};
	const calls: { step: keyof WizardSteps; prev: unknown }[] = [];
	const pop = <T>(key: keyof WizardSteps, prev: unknown): StepResult<T> => {
		calls.push({ step: key, prev });
		const r = queues[key].shift();
		if (!r) throw new Error(`queuedSteps: no result queued for ${key}`);
		return r as StepResult<T>;
	};
	const steps: WizardSteps = {
		pathArg: (prev) => Promise.resolve(pop("pathArg", prev)),
		source: (prev) => Promise.resolve(pop("source", prev)),
		sourceName: (kind, prev) =>
			Promise.resolve(pop("sourceName", { kind, prev })),
		partition: (prev) => Promise.resolve(pop("partition", prev)),
		emit: (prev) => Promise.resolve(pop("emit", prev)),
	};
	return {
		steps,
		push: (key, result) => queues[key].push(result),
		calls,
	};
}

describe("runScaffoldWizard", () => {
	it("walks the happy path with all events (4-step total)", async () => {
		const { steps, push } = queuedSteps();
		push("pathArg", { kind: "value", value: "p" });
		push("source", { kind: "value", value: "all" });
		push("partition", { kind: "value", value: "none" });
		push("emit", { kind: "value", value: false });
		const result = await runScaffoldWizard(steps);
		expect(result).toEqual({
			pathArg: "p",
			source: { kind: "all" },
			partition: "none",
			emit: false,
		});
	});

	it("walks the happy path with a category source (5-step total)", async () => {
		const { steps, push } = queuedSteps();
		push("pathArg", { kind: "value", value: "totals" });
		push("source", { kind: "value", value: "category" });
		push("sourceName", { kind: "value", value: "orders" });
		push("partition", { kind: "value", value: "per-stream" });
		push("emit", { kind: "value", value: true });
		const result = await runScaffoldWizard(steps);
		expect(result).toEqual({
			pathArg: "totals",
			source: { kind: "category", name: "orders" },
			partition: "per-stream",
			emit: true,
		});
	});

	it("preserves prior values across Back navigation", async () => {
		const { steps, push, calls } = queuedSteps();
		// Forward to step 4 (partition), Back to sourceName, then
		// forward with a different value. The second sourceName call's
		// `prev` should carry the previously-entered "orders-42" so
		// the user doesn't have to retype.
		push("pathArg", { kind: "value", value: "p" });
		push("source", { kind: "value", value: "category" });
		push("sourceName", { kind: "value", value: "orders-42" });
		push("partition", { kind: "back" });
		push("sourceName", { kind: "value", value: "orders-99" });
		push("partition", { kind: "value", value: "none" });
		push("emit", { kind: "value", value: false });
		const result = await runScaffoldWizard(steps);
		expect(result?.source).toEqual({ kind: "category", name: "orders-99" });
		const sourceNameCalls = calls.filter((c) => c.step === "sourceName");
		expect(sourceNameCalls).toHaveLength(2);
		// `prev` is the {kind, prev} bundle that queuedSteps wraps in.
		expect((sourceNameCalls[1]?.prev as { prev: unknown })?.prev).toBe(
			"orders-42",
		);
	});

	it("skips the partition step on the single-stream path and forces none", async () => {
		const { steps, push, calls } = queuedSteps();
		push("pathArg", { kind: "value", value: "p" });
		push("source", { kind: "value", value: "stream" });
		push("sourceName", { kind: "value", value: "orders-42" });
		push("emit", { kind: "value", value: false });
		const result = await runScaffoldWizard(steps);
		// per-stream partitioning is invalid with fromStream(), so the
		// step is skipped entirely and partition is forced to none.
		expect(calls.find((c) => c.step === "partition")).toBeUndefined();
		expect(result).toEqual({
			pathArg: "p",
			source: { kind: "stream", name: "orders-42" },
			partition: "none",
			emit: false,
		});
	});

	it("skips the sourceName step on the all-events path", async () => {
		const { steps, push, calls } = queuedSteps();
		push("pathArg", { kind: "value", value: "p" });
		push("source", { kind: "value", value: "all" });
		push("partition", { kind: "value", value: "none" });
		push("emit", { kind: "value", value: false });
		await runScaffoldWizard(steps);
		expect(calls.find((c) => c.step === "sourceName")).toBeUndefined();
	});

	it("Back from partition skips back to source (not sourceName) on the all-events path", async () => {
		const { steps, push, calls } = queuedSteps();
		push("pathArg", { kind: "value", value: "p" });
		push("source", { kind: "value", value: "all" });
		push("partition", { kind: "back" });
		push("source", { kind: "value", value: "all" });
		push("partition", { kind: "value", value: "none" });
		push("emit", { kind: "value", value: false });
		await runScaffoldWizard(steps);
		// Two source calls, no sourceName call.
		expect(calls.filter((c) => c.step === "source")).toHaveLength(2);
		expect(calls.find((c) => c.step === "sourceName")).toBeUndefined();
	});

	// Cancellation at each step. The state machine routes every cancel
	// to "return undefined" - parametrising the test guards against a
	// future refactor that handles one step's cancel differently
	// (e.g. confirm-discard prompt) and forgets the others.
	describe("returns undefined when the user cancels", () => {
		it("at the pathArg step", async () => {
			const { steps, push } = queuedSteps();
			push("pathArg", { kind: "cancel" });
			expect(await runScaffoldWizard(steps)).toBeUndefined();
		});
		it("at the source step", async () => {
			const { steps, push } = queuedSteps();
			push("pathArg", { kind: "value", value: "p" });
			push("source", { kind: "cancel" });
			expect(await runScaffoldWizard(steps)).toBeUndefined();
		});
		it("at the sourceName step", async () => {
			const { steps, push } = queuedSteps();
			push("pathArg", { kind: "value", value: "p" });
			push("source", { kind: "value", value: "stream" });
			push("sourceName", { kind: "cancel" });
			expect(await runScaffoldWizard(steps)).toBeUndefined();
		});
		it("at the partition step", async () => {
			const { steps, push } = queuedSteps();
			push("pathArg", { kind: "value", value: "p" });
			push("source", { kind: "value", value: "all" });
			push("partition", { kind: "cancel" });
			expect(await runScaffoldWizard(steps)).toBeUndefined();
		});
		it("at the emit step", async () => {
			const { steps, push } = queuedSteps();
			push("pathArg", { kind: "value", value: "p" });
			push("source", { kind: "value", value: "all" });
			push("partition", { kind: "value", value: "none" });
			push("emit", { kind: "cancel" });
			expect(await runScaffoldWizard(steps)).toBeUndefined();
		});
	});

	// Back from the first step is unreachable in production (step
	// 1's renderer sets hasBack: false), but the state machine
	// treats it as cancel as a defensive contract for future
	// renderers.
	it("treats Back from the first step as cancel", async () => {
		const { steps, push } = queuedSteps();
		push("pathArg", { kind: "back" });
		const result = await runScaffoldWizard(steps);
		expect(result).toBeUndefined();
	});
});
