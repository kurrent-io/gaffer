// Hand-rolled stub for the `vscode` module, aliased in vite.config.ts.
//
// Purpose-built for the surface this extension actually uses; it is not
// a faithful re-implementation of the public API. New imports require a
// new mock entry. Mutable shared state lives in `state` and is reset
// per-test via `resetVscode()` (test/testutil/vscode-state.ts).

export interface Disposable {
	dispose(): void;
}

// ---- EventEmitter ---------------------------------------------------------

export class EventEmitter<T> {
	#listeners: Array<(e: T) => void> = [];
	readonly event = (listener: (e: T) => void): Disposable => {
		this.#listeners.push(listener);
		return {
			dispose: () => {
				const i = this.#listeners.indexOf(listener);
				if (i >= 0) this.#listeners.splice(i, 1);
			},
		};
	};
	// Real vscode swallows individual listener throws so one bug doesn't
	// stop the others. Mirror that, otherwise a buggy listener masks the
	// rest in tests in a way it wouldn't in production.
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

export class Uri {
	readonly scheme: string;
	readonly path: string;
	readonly fsPath: string;
	private constructor(scheme: string, path: string) {
		this.scheme = scheme;
		this.path = path;
		this.fsPath = path;
	}
	static file(p: string): Uri {
		return new Uri("file", p);
	}
	static joinPath(base: Uri, ...segments: string[]): Uri {
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
	toString(): string {
		return `${this.scheme}://${this.path}`;
	}
}

// ---- Position / Range -----------------------------------------------------

export class Position {
	readonly line: number;
	readonly character: number;
	constructor(line: number, character: number) {
		this.line = line;
		this.character = character;
	}
}

export class Range {
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
}

// ---- Theme / TreeItem -----------------------------------------------------

export class ThemeIcon {
	readonly id: string;
	readonly color: ThemeColor | undefined;
	constructor(id: string, color?: ThemeColor) {
		this.id = id;
		this.color = color;
	}
}

export class ThemeColor {
	readonly id: string;
	constructor(id: string) {
		this.id = id;
	}
}

export const TreeItemCollapsibleState = {
	None: 0,
	Collapsed: 1,
	Expanded: 2,
} as const;

export class TreeItem {
	label?: string | { label: string };
	collapsibleState?: number;
	iconPath?: ThemeIcon;
	description?: string;
	contextValue?: string;
	constructor(
		label: string | { label: string },
		collapsibleState: number = TreeItemCollapsibleState.None,
	) {
		this.label = label;
		this.collapsibleState = collapsibleState;
	}
}

// ---- CodeLens / Diagnostics -----------------------------------------------

export class CodeLens {
	readonly range: Range;
	command?: { title: string; command: string; arguments?: unknown[] };
	constructor(
		range: Range,
		command?: { title: string; command: string; arguments?: unknown[] },
	) {
		this.range = range;
		if (command) this.command = command;
	}
}

export const DiagnosticSeverity = {
	Error: 0,
	Warning: 1,
	Information: 2,
	Hint: 3,
} as const;

export class Diagnostic {
	readonly range: Range;
	readonly message: string;
	readonly severity: number;
	source?: string;
	constructor(
		range: Range,
		message: string,
		severity: number = DiagnosticSeverity.Error,
	) {
		this.range = range;
		this.message = message;
		this.severity = severity;
	}
}

// ---- Debug ----------------------------------------------------------------

export class DebugAdapterServer {
	readonly port: number;
	constructor(port: number) {
		this.port = port;
	}
}

// ---- Output channel / Diagnostics collection ------------------------------

export interface FakeOutputChannel extends Disposable {
	readonly name: string;
	readonly languageId: string | undefined;
	lines: string[];
	clearCount: number;
	showCount: number;
	appendLine(line: string): void;
	clear(): void;
	show(): void;
}

export interface FakeDiagnosticCollection extends Disposable {
	readonly name: string;
	readonly entries: Map<string, Diagnostic[]>;
	clearCount: number;
	set(uri: Uri, diagnostics: Diagnostic[]): void;
	clear(): void;
}

// ---- Webview / View providers ---------------------------------------------

export interface FakeWebview {
	options: unknown;
	html: string;
	cspSource: string;
	postedMessages: unknown[];
	onDidReceiveMessage: EventEmitter<unknown>["event"];
	postMessage(msg: unknown): Thenable<boolean>;
	emitMessage(msg: unknown): void;
}

export interface FakeWebviewView {
	readonly webview: FakeWebview;
	readonly onDidDispose: EventEmitter<void>["event"];
	emitDispose(): void;
}

export function makeFakeWebviewView(): FakeWebviewView {
	const onDidReceiveMessage = new EventEmitter<unknown>();
	const onDidDispose = new EventEmitter<void>();
	const webview: FakeWebview = {
		options: {},
		html: "",
		cspSource: "vscode-resource:fake",
		postedMessages: [],
		onDidReceiveMessage: onDidReceiveMessage.event,
		postMessage(msg) {
			webview.postedMessages.push(msg);
			return Promise.resolve(true);
		},
		emitMessage(msg) {
			onDidReceiveMessage.fire(msg);
		},
	};
	return {
		webview,
		onDidDispose: onDidDispose.event,
		emitDispose: () => onDidDispose.fire(),
	};
}

// ---- File system watcher --------------------------------------------------

export interface FakeFileSystemWatcher extends Disposable {
	readonly pattern: string;
	onDidChange: EventEmitter<Uri>["event"];
	onDidCreate: EventEmitter<Uri>["event"];
	onDidDelete: EventEmitter<Uri>["event"];
	emitChange(uri: Uri): void;
	emitCreate(uri: Uri): void;
	emitDelete(uri: Uri): void;
}

// ---- Configuration --------------------------------------------------------

export interface ConfigurationInspectResult<T> {
	defaultValue?: T;
	globalValue?: T;
	workspaceValue?: T;
	workspaceFolderValue?: T;
}

export interface FakeConfiguration {
	get<T>(section: string, defaultValue?: T): T | undefined;
	inspect<T>(section: string): ConfigurationInspectResult<T> | undefined;
}

// ---- Mutable shared state -------------------------------------------------

interface ConfigEntry {
	value?: unknown;
	inspect?: ConfigurationInspectResult<unknown>;
}

export interface MockState {
	isTrusted: boolean;
	findFilesQueue: Uri[][];
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
	startDebuggingResult: boolean;
	startDebuggingCalls: Array<{ folder: unknown; configuration: unknown }>;
	stopDebuggingCount: number;
	configurationChanged: EventEmitter<{
		affectsConfiguration: (section: string) => boolean;
	}>;
	workspaceTrustGranted: EventEmitter<void>;
	debugStarted: EventEmitter<DebugSession>;
	debugTerminated: EventEmitter<DebugSession>;
	debugCustomEvent: EventEmitter<DebugSessionCustomEvent>;
	workspaceFolders: WorkspaceFolder[];
	asRelativePathImpl: (uri: Uri | string) => string;
}

export interface DebugSession {
	id: string;
	type: string;
	name: string;
	configuration: { [key: string]: unknown };
	customRequest(command: string, args?: unknown): Thenable<unknown>;
}

export interface DebugSessionCustomEvent {
	session: DebugSession;
	event: string;
	body: unknown;
}

export interface WorkspaceFolder {
	uri: Uri;
	name: string;
	index: number;
}

export const state: MockState = createInitialState();

function createInitialState(): MockState {
	return {
		isTrusted: true,
		findFilesQueue: [],
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
		startDebuggingResult: true,
		startDebuggingCalls: [],
		stopDebuggingCount: 0,
		configurationChanged: new EventEmitter(),
		workspaceTrustGranted: new EventEmitter(),
		debugStarted: new EventEmitter(),
		debugTerminated: new EventEmitter(),
		debugCustomEvent: new EventEmitter(),
		workspaceFolders: [],
		asRelativePathImpl: (uri) => (typeof uri === "string" ? uri : uri.fsPath),
	};
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

// ---- workspace ------------------------------------------------------------

export const workspace = {
	get isTrusted(): boolean {
		return state.isTrusted;
	},
	get workspaceFolders(): readonly WorkspaceFolder[] {
		return state.workspaceFolders;
	},
	getWorkspaceFolder(_uri: Uri): WorkspaceFolder | undefined {
		return state.workspaceFolders[0];
	},
	asRelativePath(uri: Uri | string): string {
		return state.asRelativePathImpl(uri);
	},
	async findFiles(pattern: string, exclude?: string): Promise<Uri[]> {
		state.findFilesCalls.push({ pattern, exclude });
		return state.findFilesQueue.shift() ?? [];
	},
	getConfiguration(section: string): FakeConfiguration {
		const sec = state.configurations.get(section) ?? new Map();
		return {
			get<T>(key: string, defaultValue?: T): T | undefined {
				const e = sec.get(key);
				if (!e) return defaultValue;
				return (e.value as T) ?? defaultValue;
			},
			inspect<T>(key: string): ConfigurationInspectResult<T> | undefined {
				const e = sec.get(key);
				return e?.inspect as ConfigurationInspectResult<T> | undefined;
			},
		};
	},
	createFileSystemWatcher(pattern: string): FakeFileSystemWatcher {
		const onDidChange = new EventEmitter<Uri>();
		const onDidCreate = new EventEmitter<Uri>();
		const onDidDelete = new EventEmitter<Uri>();
		const w: FakeFileSystemWatcher = {
			pattern,
			onDidChange: onDidChange.event,
			onDidCreate: onDidCreate.event,
			onDidDelete: onDidDelete.event,
			emitChange: (uri) => onDidChange.fire(uri),
			emitCreate: (uri) => onDidCreate.fire(uri),
			emitDelete: (uri) => onDidDelete.fire(uri),
			dispose() {
				onDidChange.dispose();
				onDidCreate.dispose();
				onDidDelete.dispose();
			},
		};
		state.fileWatchers.push(w);
		return w;
	},
	onDidChangeConfiguration: ((listener) =>
		state.configurationChanged.event(
			listener,
		)) as (typeof state.configurationChanged)["event"],
	onDidGrantWorkspaceTrust: ((listener) =>
		state.workspaceTrustGranted.event(
			listener,
		)) as (typeof state.workspaceTrustGranted)["event"],
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

interface QuickPickItem {
	label: string;
	description?: string;
}

export const window = {
	showErrorMessage(
		message: string,
		...items: string[]
	): Thenable<string | undefined> {
		return showMessage("error", message, items);
	},
	showWarningMessage(
		message: string,
		...items: string[]
	): Thenable<string | undefined> {
		return showMessage("warning", message, items);
	},
	showInformationMessage(
		message: string,
		...items: string[]
	): Thenable<string | undefined> {
		return showMessage("info", message, items);
	},
	async showQuickPick<T extends QuickPickItem>(
		items: T[] | Thenable<T[]>,
		options?: unknown,
	): Promise<T | undefined> {
		const resolved = await Promise.resolve(items);
		state.quickPickCalls.push({ items: resolved, options });
		const next = state.quickPickResolutions.shift();
		return next as T | undefined;
	},
	createOutputChannel(name: string, languageId?: string): FakeOutputChannel {
		const channel: FakeOutputChannel = {
			name,
			languageId,
			lines: [],
			clearCount: 0,
			showCount: 0,
			appendLine(line) {
				channel.lines.push(line);
			},
			clear() {
				channel.lines = [];
				channel.clearCount++;
			},
			show() {
				channel.showCount++;
			},
			dispose() {},
		};
		state.outputChannels.push(channel);
		return channel;
	},
	registerTreeDataProvider(id: string, provider: unknown): Disposable {
		state.registeredTreeProviders.push({ id, provider });
		return { dispose: () => {} };
	},
	registerWebviewViewProvider(id: string, provider: unknown): Disposable {
		state.registeredWebviewProviders.push({ id, provider });
		return { dispose: () => {} };
	},
};

// ---- commands -------------------------------------------------------------

export const commands = {
	registerCommand(
		name: string,
		handler: (...args: unknown[]) => unknown,
	): Disposable {
		state.registeredCommands.set(name, handler);
		return {
			dispose: () => {
				state.registeredCommands.delete(name);
			},
		};
	},
	executeCommand<T = unknown>(name: string, ...args: unknown[]): Thenable<T> {
		state.executeCommandCalls.push({ name, args });
		const handler =
			state.commandHandlers.get(name) ?? state.registeredCommands.get(name);
		const result = handler ? handler(...args) : undefined;
		return Promise.resolve(result as T);
	},
};

// ---- languages ------------------------------------------------------------

export const languages = {
	createDiagnosticCollection(name: string): FakeDiagnosticCollection {
		const collection: FakeDiagnosticCollection = {
			name,
			entries: new Map(),
			clearCount: 0,
			set(uri, diagnostics) {
				collection.entries.set(uri.fsPath, diagnostics);
			},
			clear() {
				collection.entries.clear();
				collection.clearCount++;
			},
			dispose() {},
		};
		state.diagnosticCollections.push(collection);
		return collection;
	},
	registerCodeLensProvider(selector: unknown, provider: unknown): Disposable {
		state.registeredCodeLensProviders.push({ selector, provider });
		return { dispose: () => {} };
	},
};

// ---- debug ----------------------------------------------------------------

export const debug = {
	startDebugging(folder: unknown, configuration: unknown): Thenable<boolean> {
		state.startDebuggingCalls.push({ folder, configuration });
		return Promise.resolve(state.startDebuggingResult);
	},
	stopDebugging(): Thenable<void> {
		state.stopDebuggingCount++;
		return Promise.resolve();
	},
	registerDebugAdapterDescriptorFactory(
		type: string,
		factory: unknown,
	): Disposable {
		state.registeredDebugFactories.push({ type, factory });
		return { dispose: () => {} };
	},
	onDidStartDebugSession: ((listener: (s: DebugSession) => void) =>
		state.debugStarted.event(listener)) as (typeof state.debugStarted)["event"],
	onDidTerminateDebugSession: ((listener: (s: DebugSession) => void) =>
		state.debugTerminated.event(
			listener,
		)) as (typeof state.debugTerminated)["event"],
	onDidReceiveDebugSessionCustomEvent: ((
		listener: (e: DebugSessionCustomEvent) => void,
	) =>
		state.debugCustomEvent.event(
			listener,
		)) as (typeof state.debugCustomEvent)["event"],
};
