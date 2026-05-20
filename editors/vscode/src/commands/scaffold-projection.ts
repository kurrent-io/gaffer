// gaffer.scaffold: command-palette / explorer-context flow that runs
// `gaffer scaffold <path> --source=... --partition=... [--emit]`.
//
// The wizard state machine (runScaffoldWizard) is split from the VS
// Code rendering (createVscodeWizardSteps) so the picker logic - the
// branching that's actually likely to regress - can be unit-tested
// with stub renderers instead of mocking createQuickPick /
// createInputBox event lifecycles.

import * as vscode from "vscode";
import * as path from "node:path";
import { runGafferCommand, type SpawnTelemetry } from "../discovery/cli.js";
import {
	showAutoInitDone,
	showCliCommandFailure,
	showTargetOutsideWorkspace,
	showTrustWarning,
} from "../notifications.js";
import {
	findProjectRoot,
	folderDisplayName,
	openFile,
	resolveTargetForCommand,
} from "./workspace.js";

export interface ScaffoldProjectionDeps {
	telemetry: SpawnTelemetry;
	// scaffoldProjection picks the flavor based on whether the
	// command was invoked from the explorer context (URI set ->
	// "name" flavor; user picks a filename, file lands in the
	// clicked folder) or the palette (no URI -> "path" flavor;
	// user types the full project-relative path).
	wizard: (flavor: WizardFlavor) => Promise<ScaffoldChoices | undefined>;
}

export function scaffoldProjection(
	deps: ScaffoldProjectionDeps,
): (uri?: vscode.Uri) => Promise<void> {
	return async (uri?: vscode.Uri) => {
		if (!vscode.workspace.isTrusted) {
			void showTrustWarning();
			return;
		}
		const target = await resolveTargetForCommand(
			"Add a projection in which project?",
			uri,
		);
		if (!target) return;

		// VS Code can fire the explorer-context menu on a folder
		// outside any workspace folder; we need a workspace to host
		// gaffer.toml. Hoisted above findProjectRoot so the
		// containing-workspace lookup also bounds that walk.
		const workspace = vscode.workspace.getWorkspaceFolder(target);
		if (!workspace) {
			void showTargetOutsideWorkspace();
			return;
		}

		const choices = await deps.wizard(uri ? "name" : "path");
		if (!choices) return;

		// If there's no gaffer.toml anywhere above the target, run
		// `gaffer init --yes` at the workspace root before scaffolding.
		// The user committed to "I want a projection" by completing
		// the wizard; init is the unavoidable precondition and runs
		// silently rather than forcing a separate command round-trip.
		// A brief info toast surfaces the side effect so the new
		// gaffer.toml doesn't appear unannounced.
		if (!(await findProjectRoot(target))) {
			const initResult = await runGafferCommand(
				["init", "--yes"],
				workspace.uri.fsPath,
				deps.telemetry,
				"command_palette",
			);
			if (!initResult.ok) {
				void showCliCommandFailure("init", initResult.err);
				return;
			}
			void showAutoInitDone(folderDisplayName(workspace.uri));
		}

		// Path semantics depend on how the command was invoked:
		// - Explorer-context (uri set): cwd is the clicked folder
		//   and the wizard's "name" flavor produced "<typed>.js".
		//   File lands directly in the clicked folder.
		// - Palette (no uri): cwd is the workspace folder and the
		//   wizard's "path" flavor produced whatever the user typed.
		// In both cases the basename derives the projection name,
		// so no --name flag is needed. The CLI's FindRoot walks up
		// from cwd to locate gaffer.toml (newly-created or not).
		const cwd = target.fsPath;
		const result = await runGafferCommand(
			["scaffold", choices.pathArg, ...scaffoldFlags(choices)],
			cwd,
			deps.telemetry,
			"command_palette",
		);
		if (!result.ok) {
			void showCliCommandFailure("scaffold", result.err);
			return;
		}
		await openFile(vscode.Uri.file(path.join(cwd, choices.pathArg)));
	};
}

function scaffoldFlags(choices: ScaffoldChoices): string[] {
	const flags = [
		`--source=${encodeSource(choices.source)}`,
		`--partition=${choices.partition}`,
	];
	if (choices.emit) flags.push("--emit");
	return flags;
}

function encodeSource(s: ScaffoldChoices["source"]): string {
	return s.kind === "all" ? "all" : `${s.kind}:${s.name}`;
}

// Validator for the explorer-context flavor: user supplies a bare
// name, the renderer appends `.js` and the file lands in the clicked
// folder. No separators or traversal allowed - those would either
// punch out of the clicked folder or imply a directory tree the
// "name here" framing doesn't support.
export function validateName(name: string): string | undefined {
	const trimmed = name.trim();
	if (!trimmed) return "Projection name is required";
	// Reject inputs whose stem is empty - e.g. ".js" alone would
	// survive the renderer's idempotent .js-append and create a
	// file named just ".js".
	const stem = trimmed.endsWith(".js") ? trimmed.slice(0, -3).trim() : trimmed;
	if (!stem) return "Projection name is required";
	if (/^[A-Za-z]:/.test(trimmed)) {
		return "Projection name cannot start with a Windows drive-letter";
	}
	if (trimmed.includes("/") || trimmed.includes("\\")) {
		return "Projection name cannot contain path separators";
	}
	if (trimmed.includes("..")) {
		return "Projection name cannot contain '..'";
	}
	return undefined;
}

// Validator for the palette flavor: user supplies a complete
// project-relative path with extension. Mirrors the CLI's
// scaffold.validateRelPath rules so the wizard fails fast instead
// of round-tripping a CLI error.
export function validatePath(input: string): string | undefined {
	const trimmed = input.trim();
	if (!trimmed) return "Projection path is required";
	if (/^[A-Za-z]:/.test(trimmed)) {
		return "Projection path cannot be a Windows drive-letter path";
	}
	if (trimmed.startsWith("/") || trimmed.startsWith("\\")) {
		return "Projection path must be relative to the project root";
	}
	const normalised = trimmed.replaceAll("\\", "/");
	const cleaned = normalisePath(normalised);
	if (cleaned === ".." || cleaned.startsWith("../")) {
		return "Projection path is outside the project root";
	}
	if (!cleaned.endsWith(".js")) {
		return "Projection path must end in .js";
	}
	// Reject paths whose basename is only the extension - the
	// CLI rejects these too with "missing a file name".
	const lastSlash = cleaned.lastIndexOf("/");
	const base = lastSlash >= 0 ? cleaned.slice(lastSlash + 1) : cleaned;
	if (base === ".js") {
		return "Projection path is missing a file name";
	}
	return undefined;
}

// Lightweight slash-only path.Clean, matching what the CLI's
// pathutil does. Resolves "." and ".." segments lexically; the
// result starts with ".." only if the input escapes the root.
function normalisePath(p: string): string {
	const segments: string[] = [];
	for (const seg of p.split("/")) {
		if (seg === "" || seg === ".") continue;
		if (seg === "..") {
			if (segments.length > 0 && segments[segments.length - 1] !== "..") {
				segments.pop();
			} else {
				segments.push("..");
			}
		} else {
			segments.push(seg);
		}
	}
	return segments.join("/") || ".";
}

// ---- Wizard state machine -----------------------------------------------

type SourceKind = "all" | "stream" | "category";
type Partition = "none" | "per-stream";

// `pathArg` is the literal first positional argument to `gaffer
// scaffold` - either a bare `<name>.js` (explorer-context, file
// lands in the clicked folder) or a project-root-relative path the
// user typed (palette). The renderer for step 1 produces either
// shape; the rest of the wizard doesn't care which.
export interface ScaffoldChoices {
	pathArg: string;
	source:
		| { kind: "all" }
		| { kind: "stream"; name: string }
		| { kind: "category"; name: string };
	partition: Partition;
	emit: boolean;
}

// `name` flavor asks for a bare projection identifier and appends
// `.js`; `path` flavor asks for a full project-relative path with
// extension. Picked by the caller based on invocation context.
export type WizardFlavor = "name" | "path";

export type StepResult<T> =
	| { kind: "value"; value: T }
	| { kind: "back" }
	| { kind: "cancel" };

// Each renderer gets the previously-entered value (so Back-then-forward
// preserves what the user already typed/picked) plus the current step
// position for the title bar. Injected as a struct so tests can supply
// canned responses without touching the QuickInput API.
export interface WizardSteps {
	// Step 1: produces the literal pathArg to pass to `gaffer
	// scaffold`. The renderer encodes whichever flavor (name vs path)
	// the caller chose; the state machine just stores the result.
	pathArg(
		prev: string | undefined,
		step: number,
		totalSteps: number,
	): Promise<StepResult<string>>;
	source(
		prev: SourceKind | undefined,
		step: number,
		totalSteps: number,
	): Promise<StepResult<SourceKind>>;
	sourceName(
		kind: "stream" | "category",
		prev: string | undefined,
		step: number,
		totalSteps: number,
	): Promise<StepResult<string>>;
	partition(
		prev: Partition | undefined,
		step: number,
		totalSteps: number,
	): Promise<StepResult<Partition>>;
	emit(
		prev: boolean | undefined,
		step: number,
		totalSteps: number,
	): Promise<StepResult<boolean>>;
}

export async function runScaffoldWizard(
	steps: WizardSteps,
): Promise<ScaffoldChoices | undefined> {
	let pathArg: string | undefined;
	let sourceKind: SourceKind | undefined;
	let sourceName: string | undefined;
	let partition: Partition | undefined;
	let emit: boolean | undefined;

	// step identifies the renderer directly (no overlap between paths):
	// 1=name, 2=source, 3=sourceName (skipped on the all-events path),
	// 4=partition, 5=emit. totalSteps drops to 4 the moment the user
	// picks "all"; the logical display number for partition/emit
	// shifts accordingly so the user sees a clean "1 of 4 ... 4 of 4"
	// progression instead of skipping a number.
	let step: 1 | 2 | 3 | 4 | 5 = 1;
	for (;;) {
		const total = sourceKind === "all" ? 4 : 5;
		if (step === 1) {
			const r = await steps.pathArg(pathArg, 1, total);
			if (r.kind === "cancel" || r.kind === "back") return undefined;
			pathArg = r.value;
			step = 2;
		} else if (step === 2) {
			const r = await steps.source(sourceKind, 2, total);
			if (r.kind === "cancel") return undefined;
			if (r.kind === "back") {
				step = 1;
				continue;
			}
			sourceKind = r.value;
			step = sourceKind === "all" ? 4 : 3;
		} else if (step === 3) {
			// sourceKind is stream or category whenever step===3 is
			// reached (step 3 is unreachable on the all-events path).
			const r = await steps.sourceName(
				sourceKind as "stream" | "category",
				sourceName,
				3,
				total,
			);
			if (r.kind === "cancel") return undefined;
			if (r.kind === "back") {
				step = 2;
				continue;
			}
			sourceName = r.value;
			step = 4;
		} else if (step === 4) {
			const logical = sourceKind === "all" ? 3 : 4;
			const r = await steps.partition(partition, logical, total);
			if (r.kind === "cancel") return undefined;
			if (r.kind === "back") {
				step = sourceKind === "all" ? 2 : 3;
				continue;
			}
			partition = r.value;
			step = 5;
		} else {
			const logical = sourceKind === "all" ? 4 : 5;
			const r = await steps.emit(emit, logical, total);
			if (r.kind === "cancel") return undefined;
			if (r.kind === "back") {
				step = 4;
				continue;
			}
			emit = r.value;
			return {
				pathArg: pathArg as string,
				source:
					sourceKind === "all"
						? { kind: "all" }
						: {
								kind: sourceKind as "stream" | "category",
								name: sourceName as string,
							},
				partition: partition as Partition,
				emit,
			};
		}
	}
}

// ---- VS Code rendering --------------------------------------------------

// Production renderers backed by QuickInputButtons.Back. Kept here so
// the picker copy lives next to the wizard it serves; if the rendering
// grows materially, split into a sibling file without changing the
// wizard surface.
export function createVscodeWizardSteps(flavor: WizardFlavor): WizardSteps {
	const title = "Scaffold projection";
	return {
		pathArg: async (prev, step, totalSteps) => {
			if (flavor === "name") {
				// Strip a trailing `.js` from `prev` so Back-then-forward
				// doesn't keep stacking extensions (the renderer appends
				// `.js` after each accept, but the state machine stores
				// the appended value and threads it back as `prev`).
				const seed = prev?.endsWith(".js") ? prev.slice(0, -3) : prev;
				const r = await runInputStep({
					title,
					step,
					totalSteps,
					prompt: "Projection name",
					placeholder: "e.g. order-totals",
					value: seed ?? "",
					validate: validateName,
					hasBack: false,
				});
				if (r.kind !== "value") return r;
				// Idempotent append: a user who types "foo.js"
				// shouldn't end up with "foo.js.js".
				const value = r.value.endsWith(".js") ? r.value : `${r.value}.js`;
				return { kind: "value", value };
			}
			const r = await runInputStep({
				title,
				step,
				totalSteps,
				prompt: "Projection path",
				placeholder: "e.g. projections/order-totals.js",
				value: prev ?? "",
				validate: validatePath,
				hasBack: false,
			});
			if (r.kind !== "value") return r;
			// Canonical slash-form so the CLI argv and the post-spawn
			// openFile path agree on the same string regardless of
			// how the user typed separators.
			return {
				kind: "value",
				value: normalisePath(r.value.replaceAll("\\", "/")),
			};
		},
		source: (prev, step, totalSteps) =>
			runQuickPickStep<SourceKind>({
				title,
				step,
				totalSteps,
				placeholder: "Where do events come from?",
				items: [
					{
						label: "$(globe) All events",
						description: "fromAll()",
						value: "all",
					},
					{
						label: "$(symbol-event) Specific stream",
						description: "fromStream(name)",
						value: "stream",
					},
					{
						label: "$(symbol-namespace) Category",
						description: "fromCategory(name)",
						value: "category",
					},
				],
				active: prev,
				hasBack: true,
			}),
		sourceName: (kind, prev, step, totalSteps) =>
			runInputStep({
				title,
				step,
				totalSteps,
				prompt: kind === "stream" ? "Stream name" : "Category name",
				placeholder: kind === "stream" ? "e.g. orders-42" : "e.g. orders",
				value: prev ?? "",
				validate: (v) => (v.trim() ? undefined : "Name is required"),
				hasBack: true,
			}),
		partition: (prev, step, totalSteps) =>
			runQuickPickStep<Partition>({
				title,
				step,
				totalSteps,
				placeholder: "Partition state by...",
				items: [
					{ label: "None", description: "Single shared state", value: "none" },
					{
						label: "Per stream",
						description: "foreachStream() - one state per source stream",
						value: "per-stream",
					},
				],
				active: prev,
				hasBack: true,
			}),
		emit: (prev, step, totalSteps) =>
			runQuickPickStep<boolean>({
				title,
				step,
				totalSteps,
				placeholder: "Include an emit() example?",
				items: [
					{
						label: "No emit code",
						description: "Read-only projection",
						value: false,
					},
					{
						label: "Include emit() example",
						description: "Comment showing how to emit a derived event",
						value: true,
					},
				],
				active: prev,
				hasBack: true,
			}),
	};
}

interface InputStepOptions {
	title: string;
	step: number;
	totalSteps: number;
	prompt: string;
	placeholder?: string;
	value: string;
	validate?: (v: string) => string | undefined;
	hasBack: boolean;
}

function runInputStep(opts: InputStepOptions): Promise<StepResult<string>> {
	return new Promise((resolve) => {
		const input = vscode.window.createInputBox();
		input.title = opts.title;
		input.step = opts.step;
		input.totalSteps = opts.totalSteps;
		input.prompt = opts.prompt;
		if (opts.placeholder !== undefined) input.placeholder = opts.placeholder;
		input.value = opts.value;
		input.ignoreFocusOut = true;
		if (opts.hasBack) input.buttons = [vscode.QuickInputButtons.Back];
		let settled = false;
		const settle = (r: StepResult<string>): void => {
			if (settled) return;
			settled = true;
			input.dispose();
			resolve(r);
		};
		input.onDidChangeValue(() => {
			input.validationMessage = undefined;
		});
		input.onDidTriggerButton((b) => {
			if (b === vscode.QuickInputButtons.Back) settle({ kind: "back" });
		});
		input.onDidAccept(() => {
			const value = input.value;
			if (opts.validate) {
				const err = opts.validate(value);
				if (err !== undefined) {
					input.validationMessage = err;
					return;
				}
			}
			settle({ kind: "value", value: value.trim() });
		});
		input.onDidHide(() => settle({ kind: "cancel" }));
		input.show();
	});
}

interface QuickPickStepOptions<T> {
	title: string;
	step: number;
	totalSteps: number;
	placeholder: string;
	items: { label: string; description?: string; value: T }[];
	active: T | undefined;
	hasBack: boolean;
}

function runQuickPickStep<T>(
	opts: QuickPickStepOptions<T>,
): Promise<StepResult<T>> {
	return new Promise((resolve) => {
		type Item = vscode.QuickPickItem & { value: T };
		const qp = vscode.window.createQuickPick<Item>();
		qp.title = opts.title;
		qp.step = opts.step;
		qp.totalSteps = opts.totalSteps;
		qp.placeholder = opts.placeholder;
		qp.ignoreFocusOut = true;
		qp.items = opts.items.map((i) => ({
			label: i.label,
			...(i.description !== undefined ? { description: i.description } : {}),
			value: i.value,
		}));
		if (opts.active !== undefined) {
			const match = qp.items.find((i) => i.value === opts.active);
			if (match) qp.activeItems = [match];
		}
		if (opts.hasBack) qp.buttons = [vscode.QuickInputButtons.Back];
		let settled = false;
		const settle = (r: StepResult<T>): void => {
			if (settled) return;
			settled = true;
			qp.dispose();
			resolve(r);
		};
		qp.onDidTriggerButton((b) => {
			if (b === vscode.QuickInputButtons.Back) settle({ kind: "back" });
		});
		qp.onDidAccept(() => {
			const picked = qp.selectedItems[0];
			if (!picked) {
				settle({ kind: "cancel" });
				return;
			}
			settle({ kind: "value", value: picked.value });
		});
		qp.onDidHide(() => settle({ kind: "cancel" }));
		qp.show();
	});
}
