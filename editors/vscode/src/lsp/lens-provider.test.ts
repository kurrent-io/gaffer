import * as vscode from "vscode";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LspCodeLensProvider } from "./lens-provider.js";
import {
	LanguageClient,
	clearLspRequestHandlers,
	setLspRequestHandler,
} from "../../test/__mocks__/vscode-languageclient-node.js";
import type { LanguageClient as RealLanguageClient } from "vscode-languageclient/node";
import { setTrusted } from "../../test/testutil/vscode-state.js";
import type { Manifest } from "../discovery/schemas.js";

// A stub manifest with `dev --debug` available - the lens
// emission gate that lensState delegates to via hasFlag /
// hasCommand. Tests that want the manifest gate to fail use
// null instead.
const manifestWithDebug: Manifest = {
	version: "test",
	commands: { dev: { flags: ["debug"] } },
};

function fakeLensRange(
	startLine: number,
	endLine = startLine,
): {
	start: { line: number; character: number };
	end: { line: number; character: number };
} {
	return {
		start: { line: startLine, character: 0 },
		end: { line: endLine, character: 1 },
	};
}

function fakeServerLens(intent: string, args: unknown, startLine = 4): unknown {
	return {
		range: fakeLensRange(startLine),
		command: { title: "Debug", command: "gaffer.serverCmd", arguments: [args] },
		data: { intent },
	};
}

// A non-clickable status-env roll-up as the server emits it: title only,
// empty command string, status-env intent.
function statusEnvLens(title: string, startLine = 4): unknown {
	return {
		range: fakeLensRange(startLine),
		command: { title, command: "" },
		data: { intent: "status-env" },
	};
}

// A sign-in lens as the server emits it: a Sign in action carrying the env +
// configURI, under the sign-in intent.
function signInLens(args: unknown, startLine = 4): unknown {
	return {
		range: fakeLensRange(startLine),
		command: { title: "Sign in", command: "gaffer.signIn", arguments: [args] },
		data: { intent: "sign-in" },
	};
}

function makeClient(): RealLanguageClient {
	const c = new LanguageClient("test", "test", null, null);
	return c as unknown as RealLanguageClient;
}

async function getLenses(p: LspCodeLensProvider): Promise<vscode.CodeLens[]> {
	const doc = {
		uri: vscode.Uri.file("/p/gaffer.toml"),
	} as vscode.TextDocument;
	return await p.provideCodeLenses(doc, {
		isCancellationRequested: false,

		onCancellationRequested: (() => {
			return { dispose: () => {} };
			// eslint-disable-next-line @typescript-eslint/no-explicit-any
		}) as any as vscode.CancellationToken["onCancellationRequested"],
	});
}

describe("LspCodeLensProvider", () => {
	beforeEach(() => {
		setTrusted(true);
	});

	afterEach(() => {
		clearLspRequestHandlers();
	});

	it("returns [] when no client is set", async () => {
		const p = new LspCodeLensProvider();
		expect(await getLenses(p)).toEqual([]);
	});

	it("decorates a debug-intent lens with the codicon-prefixed title and gaffer.debugProjection command", async () => {
		setLspRequestHandler("textDocument/codeLens", () => [
			fakeServerLens("debug", {
				name: "checkout",
				configURI: "file:///p/gaffer.toml",
				env: "cloud",
			}),
		]);
		const p = new LspCodeLensProvider();
		p.setClient(makeClient());
		p.setManifest(manifestWithDebug);
		const lenses = await getLenses(p);
		expect(lenses).toHaveLength(1);
		expect(lenses[0]?.command?.title).toBe("$(debug-start) Debug");
		expect(lenses[0]?.command?.command).toBe("gaffer.debugProjection");
		const args = lenses[0]?.command?.arguments?.[0] as {
			name: string;
			tomlUri: vscode.Uri;
			env?: string;
		};
		expect(args.name).toBe("checkout");
		// The resolved live target threads through to the launch arg.
		expect(args.env).toBe("cloud");
		// Compute the expected toString via the same Uri.parse so
		// the assertion adapts to the mock's normalization quirks
		// (VS Code's real Uri.parse normalizes; the mock doesn't).
		expect(args.tomlUri.toString()).toBe(
			vscode.Uri.parse("file:///p/gaffer.toml").toString(),
		);
	});

	it("hides the lens when the manifest lacks dev --debug", async () => {
		setLspRequestHandler("textDocument/codeLens", () => [
			fakeServerLens("debug", {
				name: "checkout",
				configURI: "file:///p/gaffer.toml",
			}),
		]);
		const p = new LspCodeLensProvider();
		p.setClient(makeClient());
		// No setManifest call → null manifest → hasFlag returns false.
		expect(await getLenses(p)).toEqual([]);
	});

	it("swaps to a Stop lens when the projection has an active debug session", async () => {
		setLspRequestHandler("textDocument/codeLens", () => [
			fakeServerLens("debug", {
				name: "checkout",
				configURI: "file:///p/gaffer.toml",
			}),
		]);
		const p = new LspCodeLensProvider();
		p.setClient(makeClient());
		p.setManifest(manifestWithDebug);
		p.setDebugState({ name: "checkout", status: "running" });
		const lenses = await getLenses(p);
		expect(lenses).toHaveLength(1);
		expect(lenses[0]?.command?.title).toMatch(/Debugging/);
		expect(lenses[0]?.command?.command).toBe("gaffer.stopDebug");
	});

	it("shows a 'starting (cancel)' lens during the starting phase", async () => {
		setLspRequestHandler("textDocument/codeLens", () => [
			fakeServerLens("debug", {
				name: "checkout",
				configURI: "file:///p/gaffer.toml",
			}),
		]);
		const p = new LspCodeLensProvider();
		p.setClient(makeClient());
		p.setManifest(manifestWithDebug);
		p.setDebugState({ name: "checkout", status: "starting" });
		const lenses = await getLenses(p);
		expect(lenses[0]?.command?.title).toMatch(/Starting \(cancel\)/);
		expect(lenses[0]?.command?.command).toBe("gaffer.stopDebug");
	});

	it("keeps per-fixture lenses clickable mid-session (only projection-level swaps to Stop)", async () => {
		setLspRequestHandler("textDocument/codeLens", () => [
			fakeServerLens("debug", {
				name: "checkout",
				configURI: "file:///p/gaffer.toml",
				fixture: "happy",
			}),
		]);
		const p = new LspCodeLensProvider();
		p.setClient(makeClient());
		p.setManifest(manifestWithDebug);
		p.setDebugState({ name: "checkout", status: "running" });
		const lenses = await getLenses(p);
		expect(lenses[0]?.command?.command).toBe("gaffer.debugProjection");
		const args = lenses[0]?.command?.arguments?.[0] as { fixture?: string };
		expect(args.fixture).toBe("happy");
	});

	it("swaps to a 'Trust workspace' lens on untrusted debug-intent", async () => {
		setTrusted(false);
		setLspRequestHandler("textDocument/codeLens", () => [
			fakeServerLens("debug", {
				name: "checkout",
				configURI: "file:///p/gaffer.toml",
			}),
		]);
		const p = new LspCodeLensProvider();
		p.setClient(makeClient());
		p.setManifest(manifestWithDebug);
		const lenses = await getLenses(p);
		expect(lenses[0]?.command?.title).toMatch(/Trust workspace/);
		expect(lenses[0]?.command?.command).toBe("workbench.trust.manage");
	});

	it("decorates a debug-choose lens as the dropdown variant, threading fixtures + envs", async () => {
		setLspRequestHandler("textDocument/codeLens", () => [
			fakeServerLens("debug-choose", {
				name: "checkout",
				configURI: "file:///p/gaffer.toml",
				fixtureNames: ["happy", "sad"],
				envs: [{ name: "local", default: true }, { name: "prod" }],
			}),
		]);
		const p = new LspCodeLensProvider();
		p.setClient(makeClient());
		p.setManifest(manifestWithDebug);
		const lenses = await getLenses(p);
		expect(lenses).toHaveLength(1);
		expect(lenses[0]?.command?.title).toBe("$(debug-start) Debug from...");
		expect(lenses[0]?.command?.command).toBe("gaffer.debugProjectionPick");
		const args = lenses[0]?.command?.arguments?.[0] as {
			fixtureNames: string[];
			envs: { name: string; default: boolean }[];
		};
		expect(args.fixtureNames).toEqual(["happy", "sad"]);
		// `default` defaults to false when the server omits it (prod).
		expect(args.envs).toEqual([
			{ name: "local", default: true },
			{ name: "prod", default: false },
		]);
	});

	it("hides debug-choose when the projection has an active session", async () => {
		setLspRequestHandler("textDocument/codeLens", () => [
			fakeServerLens("debug-choose", {
				name: "checkout",
				configURI: "file:///p/gaffer.toml",
				fixtureNames: ["happy"],
			}),
		]);
		const p = new LspCodeLensProvider();
		p.setClient(makeClient());
		p.setManifest(manifestWithDebug);
		p.setDebugState({ name: "checkout", status: "running" });
		expect(await getLenses(p)).toEqual([]);
	});

	it("rejects malformed debug args (server-side shape drift) without crashing", async () => {
		setLspRequestHandler("textDocument/codeLens", () => [
			// Missing `name` - schema rejects.
			fakeServerLens("debug", { configURI: "file:///p/gaffer.toml" }),
			fakeServerLens("debug", {
				name: "valid",
				configURI: "file:///p/gaffer.toml",
			}),
		]);
		const p = new LspCodeLensProvider();
		p.setClient(makeClient());
		p.setManifest(manifestWithDebug);
		const lenses = await getLenses(p);
		// Only the second lens survives.
		expect(lenses).toHaveLength(1);
		expect((lenses[0]?.command?.arguments?.[0] as { name: string }).name).toBe(
			"valid",
		);
	});

	// The malformed-URI guard lives behind the parseConfigURI try/
	// catch wrapper. The vscode mock's Uri.parse never throws (it
	// falls back to file://) so we can't exercise the catch from
	// here; the production VS Code Uri.parse with strict=true does
	// throw. Pinning would need a per-test override of vscode.Uri.

	it("renders a status-env roll-up as non-clickable text (empty command)", async () => {
		setLspRequestHandler("textDocument/codeLens", () => [
			statusEnvLens("3 projections · in sync"),
		]);
		const p = new LspCodeLensProvider();
		p.setClient(makeClient());
		const lenses = await getLenses(p);
		expect(lenses).toHaveLength(1);
		expect(lenses[0]?.command?.title).toBe("3 projections · in sync");
		// Empty command id -> VS Code renders a plain span, not a clickable link.
		expect(lenses[0]?.command?.command).toBe("");
	});

	it("drops a status-env lens with an empty title", async () => {
		setLspRequestHandler("textDocument/codeLens", () => [statusEnvLens("")]);
		const p = new LspCodeLensProvider();
		p.setClient(makeClient());
		expect(await getLenses(p)).toEqual([]);
	});

	it("passes the status-env tooltip through", async () => {
		setLspRequestHandler("textDocument/codeLens", () => [
			{
				range: fakeLensRange(4),
				command: {
					title: "3 projections · in sync",
					command: "",
					tooltip: "Target: prod-cluster",
				},
				data: { intent: "status-env" },
			},
		]);
		const p = new LspCodeLensProvider();
		p.setClient(makeClient());
		const lenses = await getLenses(p);
		expect(lenses[0]?.command?.tooltip).toBe("Target: prod-cluster");
	});

	it("decorates a sign-in lens with the key icon and gaffer.signIn command", async () => {
		setLspRequestHandler("textDocument/codeLens", () => [
			signInLens({ env: "prod", configURI: "file:///p/gaffer.toml" }),
		]);
		const p = new LspCodeLensProvider();
		p.setClient(makeClient());
		const lenses = await getLenses(p);
		expect(lenses).toHaveLength(1);
		expect(lenses[0]?.command?.title).toBe("$(key) Sign in");
		expect(lenses[0]?.command?.command).toBe("gaffer.signIn");
		const args = lenses[0]?.command?.arguments?.[0] as {
			env: string;
			tomlUri: vscode.Uri;
		};
		expect(args.env).toBe("prod");
		expect(args.tomlUri.toString()).toBe(
			vscode.Uri.parse("file:///p/gaffer.toml").toString(),
		);
	});

	it("hides the sign-in lens in an untrusted workspace", async () => {
		setTrusted(false);
		setLspRequestHandler("textDocument/codeLens", () => [
			signInLens({ env: "prod", configURI: "file:///p/gaffer.toml" }),
		]);
		const p = new LspCodeLensProvider();
		p.setClient(makeClient());
		expect(await getLenses(p)).toEqual([]);
	});

	it("rejects malformed sign-in args without crashing", async () => {
		setLspRequestHandler("textDocument/codeLens", () => [
			signInLens({ env: 123 }),
		]);
		const p = new LspCodeLensProvider();
		p.setClient(makeClient());
		expect(await getLenses(p)).toEqual([]);
	});

	it("trust-gates the unknown-intent passthrough", async () => {
		setTrusted(false);
		setLspRequestHandler("textDocument/codeLens", () => [
			fakeServerLens("future-intent-we-dont-know", {}),
		]);
		const p = new LspCodeLensProvider();
		p.setClient(makeClient());
		p.setManifest(manifestWithDebug);
		expect(await getLenses(p)).toEqual([]);
	});

	it("passes through unknown intents verbatim when trusted", async () => {
		setLspRequestHandler("textDocument/codeLens", () => [
			fakeServerLens("future-intent", { foo: "bar" }),
		]);
		const p = new LspCodeLensProvider();
		p.setClient(makeClient());
		p.setManifest(manifestWithDebug);
		const lenses = await getLenses(p);
		expect(lenses).toHaveLength(1);
		expect(lenses[0]?.command?.title).toBe("Debug");
		expect(lenses[0]?.command?.command).toBe("gaffer.serverCmd");
	});

	it("returns [] when sendRequest rejects (cancellation, transient)", async () => {
		setLspRequestHandler("textDocument/codeLens", () => {
			throw new Error("simulated transport error");
		});
		const p = new LspCodeLensProvider();
		p.setClient(makeClient());
		p.setManifest(manifestWithDebug);
		expect(await getLenses(p)).toEqual([]);
	});

	it("setClient / setManifest / setDebugState / refresh each fire onDidChangeCodeLenses", () => {
		const p = new LspCodeLensProvider();
		const fired = vi.fn();
		p.onDidChangeCodeLenses(fired);
		p.setClient(makeClient());
		p.setManifest(manifestWithDebug);
		p.setDebugState({ name: "x", status: "idle" });
		p.refresh();
		expect(fired).toHaveBeenCalledTimes(4);
	});
});
