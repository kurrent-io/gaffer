// Stub for vscode-languageclient/node. The real module
// `require("vscode")` at load time, which the production
// extension provides but tests don't need to wire up - we
// don't exercise LSP traffic in unit tests, just the
// extension's wiring around it.
//
// Tests that drive activate() trigger startLanguageClient,
// which constructs a LanguageClient from this stub. The stub
// resolves start() and stop() trivially by default; tests that
// need to exercise the dispose-during-start race or the
// auto-restart path install hooks via the helpers below.

export const TransportKind = {
	stdio: 0,
	ipc: 1,
	pipe: 2,
	socket: 3,
} as const;

export const RevealOutputChannelOn = {
	Info: 1,
	Warn: 2,
	Error: 3,
	Never: 4,
} as const;

export const CloseAction = {
	DoNotRestart: 1,
	Restart: 2,
} as const;

export const ErrorAction = {
	Continue: 1,
	Shutdown: 2,
} as const;

export type Message = unknown;
export type ErrorHandler = unknown;
export type ErrorHandlerResult = unknown;
export type CloseHandlerResult = unknown;

// Per-test request hook. Tests can install responses for
// LSP method names (e.g. "workspace/symbol") that the
// extension code calls via client.sendRequest.
const requestHandlers = new Map<string, (params: unknown) => unknown>();

export function setLspRequestHandler(
	method: string,
	handler: (params: unknown) => unknown,
): void {
	requestHandlers.set(method, handler);
}

export function clearLspRequestHandlers(): void {
	requestHandlers.clear();
}

// Records every client.sendNotification call so tests can assert
// fire-and-forget traffic (e.g. gaffer/refreshStatus).
export const sentNotifications: Array<{ method: string; params: unknown }> = [];

// Optional async gate held by `start()`. Tests install a resolver
// to exercise the dispose-during-start race in spawnLanguageClient.
let startGate: Promise<void> | null = null;
let startGateResolve: (() => void) | null = null;
export function holdLspStart(): () => void {
	startGate = new Promise<void>((r) => {
		startGateResolve = r;
	});
	return () => {
		const resolve = startGateResolve;
		startGate = null;
		startGateResolve = null;
		if (resolve) resolve();
	};
}

// Tracks every LanguageClient ever constructed under the mock.
// Lets tests drive restarts by re-invoking ServerOptions like the
// real `vscode-languageclient` does on `CloseAction.Restart`.
export const constructedClients: LanguageClient[] = [];

// The gaffer/* methods the stub server advertises via `initializeResult`, which
// the extension reads to gate capability-dependent surfaces. Defaults to the
// full set so a test that doesn't care gets a current-CLI server; a test
// exercising an older gaffer calls setLspServedMethods with fewer (or none).
const ALL_GAFFER_METHODS = [
	"gaffer/projectionDetails",
	"gaffer/refreshStatus",
	"gaffer/diffProjection",
	"gaffer/diffVersions",
	"gaffer/operateProjection",
];

let servedMethods: string[] | null = [...ALL_GAFFER_METHODS];

/** Set what the stub server advertises. Pass `null` to model a gaffer that
 * predates the capability and sends no `experimental` block at all. */
export function setLspServedMethods(methods: string[] | null): void {
	servedMethods = methods === null ? null : [...methods];
}

export function resetLspMock(): void {
	requestHandlers.clear();
	constructedClients.length = 0;
	sentNotifications.length = 0;
	startGate = null;
	startGateResolve = null;
	servedMethods = [...ALL_GAFFER_METHODS];
}

export class LanguageClient {
	id: string;
	name: string;
	serverOptions: unknown;
	clientOptions: unknown;
	// Mirrors the real client's `initializeResult`, populated once start()
	// resolves. Read by the extension to learn which gaffer/* methods the server
	// serves.
	initializeResult: unknown;
	constructor(
		id: string,
		name: string,
		serverOptions: unknown,
		clientOptions: unknown,
	) {
		this.id = id;
		this.name = name;
		this.serverOptions = serverOptions;
		this.clientOptions = clientOptions;
		constructedClients.push(this);
	}

	async start(): Promise<void> {
		if (startGate) await startGate;
		// The real client exposes initializeResult only once the handshake
		// completes, so populate it here rather than in the constructor - the
		// extension reads it right after start() resolves.
		this.initializeResult = {
			capabilities:
				servedMethods === null
					? {}
					: { experimental: { gaffer: { methods: [...servedMethods] } } },
		};
	}

	async stop(_timeout?: number): Promise<void> {
		return;
	}

	async sendRequest<T>(method: string, params?: unknown): Promise<T> {
		const handler = requestHandlers.get(method);
		if (!handler) {
			return null as T;
		}
		return handler(params) as T;
	}

	async sendNotification(method: string, params?: unknown): Promise<void> {
		sentNotifications.push({ method, params });
	}

	/** Simulates `vscode-languageclient`'s internal restart path: when
	 * the error-handler returns `CloseAction.Restart`, the library calls
	 * `serverOptions()` again. Tests use this to assert that the
	 * factory re-evaluates dynamic inputs (e.g. `--invoker-id` on a
	 * mid-session opt-out flip). */
	async simulateRestart(): Promise<unknown> {
		if (typeof this.serverOptions !== "function") {
			throw new Error(
				"serverOptions is not a factory; cannot simulate restart",
			);
		}
		const factory = this.serverOptions as () => Promise<unknown>;
		return factory();
	}
}

export type ServerOptions = unknown;
export type LanguageClientOptions = unknown;
