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
}

export type ServerOptions = unknown;
export type LanguageClientOptions = unknown;
