// Stub for vscode-languageclient/node. The real module
// `require("vscode")` at load time, which the production
// extension provides but tests don't need to wire up - we
// don't exercise LSP traffic in unit tests, just the
// extension's wiring around it.
//
// Tests that drive activate() trigger startLanguageClient,
// which constructs a LanguageClient from this stub. The stub
// resolves start() and stop() trivially so activate() doesn't
// hang.

export const TransportKind = {
	stdio: 0,
	ipc: 1,
	pipe: 2,
	socket: 3,
} as const;

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

export class LanguageClient {
	id: string;
	name: string;
	serverOptions: unknown;
	clientOptions: unknown;
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
	}

	async start(): Promise<void> {
		return;
	}

	async stop(): Promise<void> {
		return;
	}

	async sendRequest<T>(method: string, params?: unknown): Promise<T> {
		const handler = requestHandlers.get(method);
		if (!handler) {
			return null as T;
		}
		return handler(params) as T;
	}
}

export type ServerOptions = unknown;
export type LanguageClientOptions = unknown;
