// Hand-rolled stub for the `vscode` module, aliased in vite.config.ts.
//
// Each class `implements vscode.X` so tsc flags drift between the mock
// and the public API. Methods we don't use throw at runtime - if a
// production change starts calling one, tests fail loud. Mutable shared
// state lives in `state` and is reset per-test via `resetVscode()`.

import type * as vscode from "vscode";

const NOT_IMPLEMENTED = (name: string): Error =>
	new Error(`vscode mock: ${name} is not implemented`);

// ---- EventEmitter ---------------------------------------------------------

export class EventEmitter<T> implements vscode.EventEmitter<T> {
	#listeners: Array<(e: T) => unknown> = [];
	readonly event: vscode.Event<T> = ((listener, thisArgs, disposables) => {
		const bound: (e: T) => unknown = thisArgs
			? (e) => listener.call(thisArgs, e)
			: listener;
		this.#listeners.push(bound);
		const disp: vscode.Disposable = {
			dispose: () => {
				const i = this.#listeners.indexOf(bound);
				if (i >= 0) this.#listeners.splice(i, 1);
			},
		};
		if (disposables) disposables.push(disp);
		return disp;
	}) as vscode.Event<T>;
	// Wrap each listener call in try/catch so a buggy listener doesn't
	// halt subsequent listeners. The real vscode.EventEmitter doesn't
	// guarantee this directly - it's the host-side dispatcher in VS
	// Code that absorbs throws around the events it pumps. Tests would
	// otherwise see one buggy listener mask the rest, which is not the
	// runtime experience we're trying to model.
	fire(value: T): void {
		for (const fn of [...this.#listeners]) {
			try {
				fn(value);
			} catch (err) {
				console.error("EventEmitter listener threw:", err);
			}
		}
	}
	dispose(): void {
		this.#listeners = [];
	}
}

// ---- Uri ------------------------------------------------------------------

export class Uri implements vscode.Uri {
	readonly scheme: string;
	readonly authority: string = "";
	readonly path: string;
	readonly query: string = "";
	readonly fragment: string = "";
	readonly fsPath: string;
	private constructor(scheme: string, path: string) {
		this.scheme = scheme;
		this.path = path;
		this.fsPath = path;
	}
	static file(p: string): Uri {
		return new Uri("file", p);
	}
	static parse(value: string, _strict?: boolean): Uri {
		// Minimal: accept "scheme:path" or treat the whole thing as a
		// file path. Production doesn't call parse; the stub exists so
		// the type satisfies vscode.Uri's static surface.
		const m = /^([a-zA-Z][a-zA-Z0-9+\-.]*):(.*)$/.exec(value);
		return m ? new Uri(m[1] ?? "file", m[2] ?? "") : Uri.file(value);
	}
	static joinPath(base: vscode.Uri, ...segments: string[]): Uri {
		// Mirror node:path.join semantics for the cases we hit in production:
		// Uri.joinPath(tomlUri, "..") -> parent dir as a Uri.
		const parts = [base.path, ...segments];
		const joined = parts.reduce((acc, seg) => {
			if (seg === "") return acc;
			if (seg === ".") return acc;
			if (seg === "..") {
				const idx = acc.lastIndexOf("/");
				return idx <= 0 ? "/" : acc.slice(0, idx);
			}
			return acc.endsWith("/") ? `${acc}${seg}` : `${acc}/${seg}`;
		}, "");
		return Uri.file(joined);
	}
	static from(_components: {
		scheme: string;
		authority?: string;
		path?: string;
		query?: string;
		fragment?: string;
	}): Uri {
		throw NOT_IMPLEMENTED("Uri.from");
	}
	with(_change: {
		scheme?: string;
		authority?: string;
		path?: string;
		query?: string;
		fragment?: string;
	}): Uri {
		throw NOT_IMPLEMENTED("Uri.with");
	}
	toString(_skipEncoding?: boolean): string {
		return `${this.scheme}://${this.path}`;
	}
	toJSON(): unknown {
		return {
			scheme: this.scheme,
			path: this.path,
			fsPath: this.fsPath,
		};
	}
}

// ---- Position / Range -----------------------------------------------------

export class Position implements vscode.Position {
	readonly line: number;
	readonly character: number;
	constructor(line: number, character: number) {
		this.line = line;
		this.character = character;
	}
	isBefore(other: vscode.Position): boolean {
		return (
			this.line < other.line ||
			(this.line === other.line && this.character < other.character)
		);
	}
	isBeforeOrEqual(other: vscode.Position): boolean {
		return this.isBefore(other) || this.isEqual(other);
	}
	isAfter(other: vscode.Position): boolean {
		return !this.isBeforeOrEqual(other);
	}
	isAfterOrEqual(other: vscode.Position): boolean {
		return !this.isBefore(other);
	}
	isEqual(other: vscode.Position): boolean {
		return this.line === other.line && this.character === other.character;
	}
	compareTo(other: vscode.Position): number {
		if (this.isBefore(other)) return -1;
		if (this.isAfter(other)) return 1;
		return 0;
	}
	translate(
		_lineDeltaOrChange?:
			| number
			| { lineDelta?: number; characterDelta?: number },
		_characterDelta?: number,
	): Position {
		throw NOT_IMPLEMENTED("Position.translate");
	}
	with(
		_lineOrChange?: number | { line?: number; character?: number },
		_character?: number,
	): Position {
		throw NOT_IMPLEMENTED("Position.with");
	}
}

export class Range implements vscode.Range {
	readonly start: Position;
	readonly end: Position;
	constructor(
		startLineOrStart: number | Position,
		startCharacterOrEnd: number | Position,
		endLine?: number,
		endCharacter?: number,
	) {
		if (
			typeof startLineOrStart === "number" &&
			typeof startCharacterOrEnd === "number"
		) {
			this.start = new Position(startLineOrStart, startCharacterOrEnd);
			this.end = new Position(endLine ?? 0, endCharacter ?? 0);
		} else if (
			startLineOrStart instanceof Position &&
			startCharacterOrEnd instanceof Position
		) {
			this.start = startLineOrStart;
			this.end = startCharacterOrEnd;
		} else {
			throw new Error("invalid Range arguments");
		}
	}
	get isEmpty(): boolean {
		return this.start.isEqual(this.end);
	}
	get isSingleLine(): boolean {
		return this.start.line === this.end.line;
	}
	contains(_positionOrRange: vscode.Position | vscode.Range): boolean {
		throw NOT_IMPLEMENTED("Range.contains");
	}
	isEqual(other: vscode.Range): boolean {
		return this.start.isEqual(other.start) && this.end.isEqual(other.end);
	}
	intersection(_range: vscode.Range): Range | undefined {
		throw NOT_IMPLEMENTED("Range.intersection");
	}
	union(_other: vscode.Range): Range {
		throw NOT_IMPLEMENTED("Range.union");
	}
	with(
		_startOrChange?:
			| vscode.Position
			| { start?: vscode.Position; end?: vscode.Position },
		_end?: vscode.Position,
	): Range {
		throw NOT_IMPLEMENTED("Range.with");
	}
}

// ---- Theme / TreeItem -----------------------------------------------------

export class ThemeIcon implements vscode.ThemeIcon {
	static readonly File = new ThemeIcon("file");
	static readonly Folder = new ThemeIcon("folder");
	readonly id: string;
	readonly color?: ThemeColor;
	constructor(id: string, color?: ThemeColor) {
		this.id = id;
		if (color !== undefined) this.color = color;
	}
}

export class ThemeColor implements vscode.ThemeColor {
	readonly id: string;
	constructor(id: string) {
		this.id = id;
	}
}

// vscode.TreeItemCollapsibleState is a numeric enum at runtime. We re-
// declare with the same numeric values; the field types below cast to
// the real enum so consumers get the right type.
export const TreeItemCollapsibleState = {
	None: 0,
	Collapsed: 1,
	Expanded: 2,
} as const;

export class TreeItem implements vscode.TreeItem {
	label?: string | vscode.TreeItemLabel;
	collapsibleState?: vscode.TreeItemCollapsibleState;
	iconPath?: ThemeIcon;
	description?: string;
	contextValue?: string;
	tooltip?: string | vscode.MarkdownString;
	command?: vscode.Command;
	id?: string;
	resourceUri?: vscode.Uri;
	accessibilityInformation?: vscode.AccessibilityInformation;
	checkboxState?:
		| vscode.TreeItemCheckboxState
		| {
				readonly state: vscode.TreeItemCheckboxState;
				readonly tooltip?: string;
		  };
	constructor(
		label: string | vscode.TreeItemLabel,
		collapsibleState: number = TreeItemCollapsibleState.None,
	) {
		this.label = label;
		this.collapsibleState = collapsibleState as vscode.TreeItemCollapsibleState;
	}
}

// ---- CodeLens / Diagnostics -----------------------------------------------

export class CodeLens implements vscode.CodeLens {
	readonly range: Range;
	command?: vscode.Command;
	constructor(range: Range, command?: vscode.Command) {
		this.range = range;
		if (command) this.command = command;
	}
	get isResolved(): boolean {
		return this.command != null;
	}
}

export const DiagnosticSeverity = {
	Error: 0,
	Warning: 1,
	Information: 2,
	Hint: 3,
} as const;

export class CodeActionKind {
	static readonly Empty = new CodeActionKind("");
	static readonly QuickFix = new CodeActionKind("quickfix");
	static readonly Refactor = new CodeActionKind("refactor");
	static readonly Source = new CodeActionKind("source");
	readonly value: string;
	constructor(value: string) {
		this.value = value;
	}
	append(parts: string): CodeActionKind {
		return new CodeActionKind(`${this.value}.${parts}`);
	}
	// Stubbed: real CodeActionKind does value-prefix containment, but
	// no test currently exercises filtering via `only`. If a provider
	// starts branching on `context.only`, implement these properly.
	intersects(_other: CodeActionKind): boolean {
		return false;
	}
	contains(_other: CodeActionKind): boolean {
		return false;
	}
}

export class CodeAction {
	command?: vscode.Command;
	diagnostics?: vscode.Diagnostic[];
	edit?: vscode.WorkspaceEdit;
	isPreferred?: boolean;
	kind?: CodeActionKind;
	readonly title: string;
	constructor(title: string, kind?: CodeActionKind) {
		this.title = title;
		if (kind) this.kind = kind;
	}
}

export class Diagnostic implements vscode.Diagnostic {
	readonly range: Range;
	readonly message: string;
	readonly severity: vscode.DiagnosticSeverity;
	source?: string;
	code?: string | number | { value: string | number; target: vscode.Uri };
	relatedInformation?: vscode.DiagnosticRelatedInformation[];
	tags?: vscode.DiagnosticTag[];
	constructor(
		range: Range,
		message: string,
		severity: number = DiagnosticSeverity.Error,
	) {
		this.range = range;
		this.message = message;
		this.severity = severity as vscode.DiagnosticSeverity;
	}
}

// ---- Debug ----------------------------------------------------------------

export class DebugAdapterServer implements vscode.DebugAdapterServer {
	readonly port: number;
	readonly host?: string;
	constructor(port: number, host?: string) {
		this.port = port;
		if (host !== undefined) this.host = host;
	}
}

// ---- Output channel / Diagnostics collection ------------------------------

export interface FakeOutputChannel extends vscode.OutputChannel {
	languageId: string | undefined;
	lines: string[];
	clearCount: number;
	showCount: number;
}

function makeOutputChannel(
	name: string,
	languageId: string | undefined,
): FakeOutputChannel {
	const channel: FakeOutputChannel = {
		name,
		languageId,
		lines: [],
		clearCount: 0,
		showCount: 0,
		append(value: string): void {
			channel.lines.push(value);
		},
		appendLine(line: string): void {
			channel.lines.push(line);
		},
		replace(value: string): void {
			channel.lines = [value];
		},
		clear(): void {
			channel.lines = [];
			channel.clearCount++;
		},
		show(
			_columnOrPreserveFocus?: vscode.ViewColumn | boolean,
			_preserveFocus?: boolean,
		): void {
			channel.showCount++;
		},
		hide(): void {},
		dispose(): void {},
	};
	return channel;
}

export interface FakeDiagnosticCollection extends vscode.DiagnosticCollection {
	readonly entries: Map<string, vscode.Diagnostic[]>;
	clearCount: number;
}

function makeDiagnosticCollection(name: string): FakeDiagnosticCollection {
	const entries = new Map<string, vscode.Diagnostic[]>();
	const collection: FakeDiagnosticCollection = {
		name,
		entries,
		clearCount: 0,
		set(
			uriOrEntries:
				| vscode.Uri
				| ReadonlyArray<[vscode.Uri, readonly vscode.Diagnostic[] | undefined]>,
			diagnostics?: readonly vscode.Diagnostic[],
		): void {
			if (Array.isArray(uriOrEntries)) {
				for (const [u, d] of uriOrEntries) {
					if (d) entries.set(u.fsPath, [...d]);
					else entries.delete(u.fsPath);
				}
			} else {
				const u = uriOrEntries as vscode.Uri;
				entries.set(u.fsPath, diagnostics ? [...diagnostics] : []);
			}
		},
		delete(uri: vscode.Uri): void {
			entries.delete(uri.fsPath);
		},
		clear(): void {
			entries.clear();
			collection.clearCount++;
		},
		forEach(
			callback: (
				uri: vscode.Uri,
				diagnostics: readonly vscode.Diagnostic[],
				collection: vscode.DiagnosticCollection,
			) => unknown,
			thisArg?: unknown,
		): void {
			for (const [k, v] of entries) {
				callback.call(thisArg, Uri.file(k), v, collection);
			}
		},
		get(uri: vscode.Uri): readonly vscode.Diagnostic[] | undefined {
			return entries.get(uri.fsPath);
		},
		has(uri: vscode.Uri): boolean {
			return entries.has(uri.fsPath);
		},
		dispose(): void {},
		[Symbol.iterator](): Iterator<[vscode.Uri, readonly vscode.Diagnostic[]]> {
			const it = entries[Symbol.iterator]();
			return {
				next(): IteratorResult<[vscode.Uri, readonly vscode.Diagnostic[]]> {
					const r = it.next();
					if (r.done) return { done: true, value: undefined };
					return { done: false, value: [Uri.file(r.value[0]), r.value[1]] };
				},
			};
		},
	};
	return collection;
}

// ---- Webview / View providers ---------------------------------------------

export interface FakeWebview extends vscode.Webview {
	postedMessages: unknown[];
	emitMessage(msg: unknown): void;
}

export interface FakeWebviewView extends vscode.WebviewView {
	readonly webview: FakeWebview;
	emitDispose(): void;
}

export function makeFakeWebviewView(): FakeWebviewView {
	const onDidReceiveMessage = new EventEmitter<unknown>();
	const onDidDispose = new EventEmitter<void>();
	const onDidChangeVisibility = new EventEmitter<void>();
	const webview: FakeWebview = {
		options: {},
		html: "",
		cspSource: "vscode-resource:fake",
		postedMessages: [],
		onDidReceiveMessage: onDidReceiveMessage.event,
		postMessage(msg: unknown): Thenable<boolean> {
			webview.postedMessages.push(msg);
			return Promise.resolve(true);
		},
		emitMessage(msg: unknown): void {
			onDidReceiveMessage.fire(msg);
		},
		asWebviewUri(localResource: vscode.Uri): vscode.Uri {
			return localResource;
		},
	};
	const view: FakeWebviewView = {
		viewType: "fake",
		webview,
		visible: true,
		onDidDispose: onDidDispose.event,
		onDidChangeVisibility: onDidChangeVisibility.event,
		show(_preserveFocus?: boolean): void {},
		emitDispose(): void {
			onDidDispose.fire();
		},
	};
	return view;
}

// ---- File system watcher --------------------------------------------------

export interface FakeFileSystemWatcher extends vscode.FileSystemWatcher {
	readonly pattern: string;
	emitChange(uri: vscode.Uri): void;
	emitCreate(uri: vscode.Uri): void;
	emitDelete(uri: vscode.Uri): void;
}

function makeFileSystemWatcher(pattern: string): FakeFileSystemWatcher {
	const onDidChange = new EventEmitter<vscode.Uri>();
	const onDidCreate = new EventEmitter<vscode.Uri>();
	const onDidDelete = new EventEmitter<vscode.Uri>();
	return {
		pattern,
		ignoreCreateEvents: false,
		ignoreChangeEvents: false,
		ignoreDeleteEvents: false,
		onDidChange: onDidChange.event,
		onDidCreate: onDidCreate.event,
		onDidDelete: onDidDelete.event,
		emitChange: (uri) => onDidChange.fire(uri),
		emitCreate: (uri) => onDidCreate.fire(uri),
		emitDelete: (uri) => onDidDelete.fire(uri),
		dispose(): void {
			onDidChange.dispose();
			onDidCreate.dispose();
			onDidDelete.dispose();
		},
	};
}

// ---- Configuration --------------------------------------------------------

interface ConfigEntry {
	value?: unknown;
	inspect?: ConfigInspectAny;
}

type ConfigInspectAny = {
	defaultValue?: unknown;
	globalValue?: unknown;
	workspaceValue?: unknown;
	workspaceFolderValue?: unknown;
};

type InspectResult<T> = {
	key: string;
	defaultValue?: T;
	globalValue?: T;
	workspaceValue?: T;
	workspaceFolderValue?: T;
};

function makeConfiguration(section: string): vscode.WorkspaceConfiguration {
	const sec = state.configurations.get(section) ?? new Map();
	const config: vscode.WorkspaceConfiguration = {
		get<T>(key: string, defaultValue?: T): T | undefined {
			const e = sec.get(key);
			if (!e) return defaultValue;
			return (e.value as T) ?? defaultValue;
		},
		has(key: string): boolean {
			return sec.has(key);
		},
		inspect<T>(key: string): InspectResult<T> | undefined {
			const e = sec.get(key);
			if (!e?.inspect) return undefined;
			return { key, ...(e.inspect as Omit<InspectResult<T>, "key">) };
		},
		update(): Thenable<void> {
			throw NOT_IMPLEMENTED("WorkspaceConfiguration.update");
		},
	};
	return config;
}

// ---- Mutable shared state -------------------------------------------------

export interface MockState {
	isTrusted: boolean;
	findFilesQueue: vscode.Uri[][];
	findFilesGates: Array<Promise<void>>;
	findFilesCalls: Array<{ pattern: string; exclude: string | undefined }>;
	configurations: Map<string, Map<string, ConfigEntry>>;
	registeredCommands: Map<string, (...args: unknown[]) => unknown>;
	executeCommandCalls: Array<{ name: string; args: unknown[] }>;
	// commandHandlers wins over registeredCommands for the same name -
	// used to stub built-ins like `setContext`. If a test registers a
	// handler for a name the extension also registers via
	// registerCommand, the test handler shadows it.
	commandHandlers: Map<string, (...args: unknown[]) => unknown>;
	quickPickResolutions: unknown[];
	quickPickCalls: Array<{ items: unknown; options: unknown }>;
	messageResolutions: unknown[];
	outputChannels: FakeOutputChannel[];
	diagnosticCollections: FakeDiagnosticCollection[];
	fileWatchers: FakeFileSystemWatcher[];
	registeredTreeProviders: Array<{ id: string; provider: unknown }>;
	registeredWebviewProviders: Array<{ id: string; provider: unknown }>;
	registeredCodeLensProviders: Array<{ selector: unknown; provider: unknown }>;
	registeredDebugFactories: Array<{ type: string; factory: unknown }>;
	registeredDebugTrackerFactories: Array<{ type: string; factory: unknown }>;
	startDebuggingResult: boolean;
	startDebuggingCalls: Array<{ folder: unknown; configuration: unknown }>;
	stopDebuggingCount: number;
	lastStartedDebugSession: vscode.DebugSession | null;
	configurationChanged: EventEmitter<vscode.ConfigurationChangeEvent>;
	workspaceTrustGranted: EventEmitter<void>;
	textDocumentChanged: EventEmitter<vscode.TextDocumentChangeEvent>;
	registeredCodeActionProviders: Array<{
		selector: unknown;
		provider: unknown;
	}>;
	debugStarted: EventEmitter<vscode.DebugSession>;
	debugTerminated: EventEmitter<vscode.DebugSession>;
	debugCustomEvent: EventEmitter<vscode.DebugSessionCustomEvent>;
	workspaceFolders: vscode.WorkspaceFolder[];
	asRelativePathImpl: (
		uri: vscode.Uri | string,
		includeWorkspaceFolder?: boolean,
	) => string;
	extensions: Map<string, vscode.Extension<unknown>>;
}

export const state: MockState = createInitialState();

function createInitialState(): MockState {
	return {
		isTrusted: true,
		findFilesQueue: [],
		findFilesGates: [],
		findFilesCalls: [],
		configurations: new Map(),
		registeredCommands: new Map(),
		executeCommandCalls: [],
		commandHandlers: new Map(),
		quickPickResolutions: [],
		quickPickCalls: [],
		messageResolutions: [],
		outputChannels: [],
		diagnosticCollections: [],
		fileWatchers: [],
		registeredTreeProviders: [],
		registeredWebviewProviders: [],
		registeredCodeLensProviders: [],
		registeredDebugFactories: [],
		registeredDebugTrackerFactories: [],
		startDebuggingResult: true,
		startDebuggingCalls: [],
		stopDebuggingCount: 0,
		lastStartedDebugSession: null,
		configurationChanged: new EventEmitter(),
		workspaceTrustGranted: new EventEmitter(),
		textDocumentChanged: new EventEmitter(),
		registeredCodeActionProviders: [],
		debugStarted: new EventEmitter(),
		debugTerminated: new EventEmitter(),
		debugCustomEvent: new EventEmitter(),
		workspaceFolders: [],
		asRelativePathImpl: (uri) => (typeof uri === "string" ? uri : uri.fsPath),
		extensions: new Map(),
	};
}

// setExtension registers a fake vscode.Extension for tests that need
// vscode.extensions.getExtension to resolve. Pass `exports` to control
// what the extension's API surface looks like; pass undefined for an
// extension-not-installed scenario by simply not calling this helper.
export function setExtension(
	id: string,
	exports: unknown,
	options: { isActive?: boolean } = {},
): vscode.Extension<unknown> {
	const ext: vscode.Extension<unknown> = {
		id,
		extensionUri: { scheme: "file", fsPath: `/fake/${id}` } as vscode.Uri,
		extensionPath: `/fake/${id}`,
		isActive: options.isActive ?? true,
		packageJSON: {},
		extensionKind: 1 as vscode.ExtensionKind,
		exports,
		activate: async () => {
			(ext as { isActive: boolean }).isActive = true;
			return exports;
		},
	};
	state.extensions.set(id, ext);
	return ext;
}

// Wholesale state replacement; keeps the exported `state` reference
// stable so importers (production code holding `vscode.workspace`)
// continue to read live values.
//
// Contract: every workspace/window/commands/debug/languages export below
// re-derefs `state` lazily on each call (closures, getters, or method
// bodies). No production-side code may capture the inner EventEmitter
// or other state field directly - if it does, this reset will leak the
// previous instance and listeners will fire into ghosts.
export function __resetState(): void {
	const next = createInitialState();
	Object.assign(state, next);
}

// ---- Aliases re-exported as values ---------------------------------------
//
// vscode.d.ts declares Disposable, CancellationToken etc. as values
// too. Re-export the bits production code uses so both `import * as
// vscode` and direct named imports work.

export const Disposable = {
	from(...items: { dispose(): unknown }[]): vscode.Disposable {
		return {
			dispose: () => {
				for (const i of items) i.dispose();
			},
		};
	},
};

// ---- workspace ------------------------------------------------------------
//
// Each namespace export below is typed as Pick<typeof vscode.X, ...>.
// This catches *signature* drift between the mock and the real API for
// the methods we DO mock - e.g. if @types/vscode changes findFiles to
// take a CancellationToken, the mock fails to compile.
//
// It does NOT catch production code reaching for an *unmocked*
// namespace method. Production imports "vscode" -> @types/vscode (no
// `paths` override in tsconfig); the alias is runtime-only. So
// `vscode.window.someUnmockedMethod()` will type-check and then throw
// at runtime with a TypeError. If we ever care, a test-only tsconfig
// with a `paths` override would close this.

type WorkspaceShape = Pick<
	typeof vscode.workspace,
	| "isTrusted"
	| "workspaceFolders"
	| "getWorkspaceFolder"
	| "asRelativePath"
	| "findFiles"
	| "getConfiguration"
	| "createFileSystemWatcher"
	| "onDidChangeConfiguration"
	| "onDidGrantWorkspaceTrust"
	| "onDidChangeTextDocument"
>;

export const workspace: WorkspaceShape = {
	get isTrusted(): boolean {
		return state.isTrusted;
	},
	get workspaceFolders(): readonly vscode.WorkspaceFolder[] | undefined {
		return state.workspaceFolders.length === 0
			? undefined
			: state.workspaceFolders;
	},
	getWorkspaceFolder(_uri: vscode.Uri): vscode.WorkspaceFolder | undefined {
		// Single-folder assumption: tests don't exercise the multi-folder
		// case. If they ever do, match by uri prefix here.
		return state.workspaceFolders[0];
	},
	asRelativePath(
		uri: vscode.Uri | string,
		_includeWorkspaceFolder?: boolean,
	): string {
		return state.asRelativePathImpl(uri);
	},
	async findFiles(
		include: vscode.GlobPattern,
		exclude?: vscode.GlobPattern | null,
	): Promise<vscode.Uri[]> {
		state.findFilesCalls.push({
			pattern: typeof include === "string" ? include : String(include),
			exclude:
				exclude == null
					? undefined
					: typeof exclude === "string"
						? exclude
						: String(exclude),
		});
		// Optional per-call gate: tests that need to prove ordering of
		// concurrent vs serialised reloads push one promise per expected
		// findFiles call. The call awaits the gate before returning. With
		// no gate set, behaves as before.
		const gate = state.findFilesGates.shift();
		if (gate) await gate;
		return state.findFilesQueue.shift() ?? [];
	},
	getConfiguration(section?: string): vscode.WorkspaceConfiguration {
		return makeConfiguration(section ?? "");
	},
	createFileSystemWatcher(
		globPattern: vscode.GlobPattern,
	): vscode.FileSystemWatcher {
		const w = makeFileSystemWatcher(
			typeof globPattern === "string" ? globPattern : String(globPattern),
		);
		state.fileWatchers.push(w);
		return w;
	},
	onDidChangeConfiguration: ((listener, thisArgs, disposables) =>
		state.configurationChanged.event(
			listener,
			thisArgs,
			disposables,
		)) as typeof vscode.workspace.onDidChangeConfiguration,
	onDidGrantWorkspaceTrust: ((listener, thisArgs, disposables) =>
		state.workspaceTrustGranted.event(
			listener,
			thisArgs,
			disposables,
		)) as typeof vscode.workspace.onDidGrantWorkspaceTrust,
	onDidChangeTextDocument: ((listener, thisArgs, disposables) =>
		state.textDocumentChanged.event(
			listener,
			thisArgs,
			disposables,
		)) as typeof vscode.workspace.onDidChangeTextDocument,
};

// ---- window ---------------------------------------------------------------

interface ShownMessage {
	kind: "error" | "warning" | "info";
	message: string;
	items: string[];
}
const shownMessages: ShownMessage[] = [];
export function __getShownMessages(): readonly ShownMessage[] {
	return shownMessages;
}
export function __clearShownMessages(): void {
	shownMessages.length = 0;
}

function showMessage(
	kind: ShownMessage["kind"],
	message: string,
	items: string[],
): Thenable<string | undefined> {
	shownMessages.push({ kind, message, items });
	const next = state.messageResolutions.shift();
	return Promise.resolve(next as string | undefined);
}

// `createOutputChannel` is overloaded in the real type. We deliberately
// only support the (name, languageId?) form, so we drop it from the
// Pick and add a narrower signature manually below.
type WindowShape = Pick<
	typeof vscode.window,
	| "showErrorMessage"
	| "showWarningMessage"
	| "showInformationMessage"
	| "showQuickPick"
	| "registerTreeDataProvider"
	| "registerWebviewViewProvider"
> & {
	createOutputChannel(name: string, languageId?: string): vscode.OutputChannel;
};

export const window: WindowShape = {
	showErrorMessage: ((
		message: string,
		...items: (string | vscode.MessageItem | vscode.MessageOptions)[]
	) =>
		showMessage(
			"error",
			message,
			items.filter((i): i is string => typeof i === "string"),
		)) as typeof vscode.window.showErrorMessage,
	showWarningMessage: ((
		message: string,
		...items: (string | vscode.MessageItem | vscode.MessageOptions)[]
	) =>
		showMessage(
			"warning",
			message,
			items.filter((i): i is string => typeof i === "string"),
		)) as typeof vscode.window.showWarningMessage,
	showInformationMessage: ((
		message: string,
		...items: (string | vscode.MessageItem | vscode.MessageOptions)[]
	) =>
		showMessage(
			"info",
			message,
			items.filter((i): i is string => typeof i === "string"),
		)) as typeof vscode.window.showInformationMessage,
	showQuickPick: (async (
		items: readonly unknown[] | Thenable<readonly unknown[]>,
		options?: unknown,
	) => {
		const resolved = await Promise.resolve(items);
		state.quickPickCalls.push({ items: resolved, options });
		return state.quickPickResolutions.shift();
	}) as typeof vscode.window.showQuickPick,
	createOutputChannel(name: string, languageId?: string): vscode.OutputChannel {
		const channel = makeOutputChannel(name, languageId);
		state.outputChannels.push(channel);
		return channel;
	},
	registerTreeDataProvider<T>(
		viewId: string,
		provider: vscode.TreeDataProvider<T>,
	): vscode.Disposable {
		state.registeredTreeProviders.push({ id: viewId, provider });
		return { dispose: () => {} };
	},
	registerWebviewViewProvider(
		viewId: string,
		provider: vscode.WebviewViewProvider,
	): vscode.Disposable {
		state.registeredWebviewProviders.push({ id: viewId, provider });
		return { dispose: () => {} };
	},
};

// ---- commands -------------------------------------------------------------

type CommandsShape = Pick<
	typeof vscode.commands,
	"registerCommand" | "executeCommand"
>;

export const commands: CommandsShape = {
	registerCommand(
		command: string,
		callback: (...args: unknown[]) => unknown,
	): vscode.Disposable {
		state.registeredCommands.set(command, callback);
		return {
			dispose: () => {
				state.registeredCommands.delete(command);
			},
		};
	},
	// Built-in VS Code commands (setContext, workbench.*, etc.) silently
	// resolve to undefined unless a test stubs them via setCommandHandler.
	// Production code that reads the resolved value for one of these
	// (none does today) would see undefined - tests should stub
	// explicitly.
	executeCommand: (<T>(name: string, ...args: unknown[]) => {
		state.executeCommandCalls.push({ name, args });
		const handler =
			state.commandHandlers.get(name) ?? state.registeredCommands.get(name);
		const result = handler ? handler(...args) : undefined;
		return Promise.resolve(result as T);
	}) as typeof vscode.commands.executeCommand,
};

// ---- languages ------------------------------------------------------------

type LanguagesShape = Pick<
	typeof vscode.languages,
	| "createDiagnosticCollection"
	| "registerCodeLensProvider"
	| "registerCodeActionsProvider"
>;

export const languages: LanguagesShape = {
	createDiagnosticCollection(name?: string): vscode.DiagnosticCollection {
		const collection = makeDiagnosticCollection(name ?? "");
		state.diagnosticCollections.push(collection);
		return collection;
	},
	registerCodeLensProvider(
		selector: vscode.DocumentSelector,
		provider: vscode.CodeLensProvider,
	): vscode.Disposable {
		state.registeredCodeLensProviders.push({ selector, provider });
		return { dispose: () => {} };
	},
	registerCodeActionsProvider(
		selector: vscode.DocumentSelector,
		provider: vscode.CodeActionProvider,
		_metadata?: vscode.CodeActionProviderMetadata,
	): vscode.Disposable {
		state.registeredCodeActionProviders.push({ selector, provider });
		return { dispose: () => {} };
	},
};

// ---- debug ----------------------------------------------------------------

let nextDebugSessionId = 0;

type DebugShape = Pick<
	typeof vscode.debug,
	| "startDebugging"
	| "stopDebugging"
	| "registerDebugAdapterDescriptorFactory"
	| "registerDebugAdapterTrackerFactory"
	| "onDidStartDebugSession"
	| "onDidTerminateDebugSession"
	| "onDidReceiveDebugSessionCustomEvent"
>;

export const debug: DebugShape = {
	startDebugging: (async (
		folder: vscode.WorkspaceFolder | undefined,
		configuration: string | vscode.DebugConfiguration,
	) => {
		state.startDebuggingCalls.push({ folder, configuration });
		if (!state.startDebuggingResult) return false;
		// Mirror production order: VS Code fires onDidStartDebugSession
		// while startDebugging is in flight, before resolving the promise.
		const config = (
			typeof configuration === "string"
				? { type: configuration, name: configuration, request: "attach" }
				: configuration
		) as vscode.DebugConfiguration;
		const session: vscode.DebugSession = {
			id: `dbg-${++nextDebugSessionId}`,
			type: config.type,
			name: config.name,
			workspaceFolder: folder,
			configuration: config,
			customRequest: () => Promise.resolve(undefined),
			getDebugProtocolBreakpoint: () => Promise.resolve(undefined),
		};
		state.lastStartedDebugSession = session;
		state.debugStarted.fire(session);
		return true;
	}) as typeof vscode.debug.startDebugging,
	stopDebugging(_session?: vscode.DebugSession): Thenable<void> {
		state.stopDebuggingCount++;
		return Promise.resolve();
	},
	registerDebugAdapterDescriptorFactory(
		debugType: string,
		factory: vscode.DebugAdapterDescriptorFactory,
	): vscode.Disposable {
		state.registeredDebugFactories.push({ type: debugType, factory });
		return { dispose: () => {} };
	},
	registerDebugAdapterTrackerFactory(
		debugType: string,
		factory: vscode.DebugAdapterTrackerFactory,
	): vscode.Disposable {
		state.registeredDebugTrackerFactories.push({ type: debugType, factory });
		return { dispose: () => {} };
	},
	onDidStartDebugSession: ((listener, thisArgs, disposables) =>
		state.debugStarted.event(
			listener,
			thisArgs,
			disposables,
		)) as typeof vscode.debug.onDidStartDebugSession,
	onDidTerminateDebugSession: ((listener, thisArgs, disposables) =>
		state.debugTerminated.event(
			listener,
			thisArgs,
			disposables,
		)) as typeof vscode.debug.onDidTerminateDebugSession,
	onDidReceiveDebugSessionCustomEvent: ((listener, thisArgs, disposables) =>
		state.debugCustomEvent.event(
			listener,
			thisArgs,
			disposables,
		)) as typeof vscode.debug.onDidReceiveDebugSessionCustomEvent,
};

type ExtensionsShape = Pick<typeof vscode.extensions, "getExtension">;

export const extensions: ExtensionsShape = {
	getExtension: ((id: string) =>
		state.extensions.get(id)) as typeof vscode.extensions.getExtension,
};
