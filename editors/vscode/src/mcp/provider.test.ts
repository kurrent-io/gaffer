import * as vscode from "vscode";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { GafferMcpProvider } from "./provider.js";
import {
	resetVscode,
	setConfiguration,
	setTrusted,
	setWorkspaceFolders,
} from "../../test/testutil/vscode-state.js";

function makeFolder(name: string, fsPath: string): vscode.WorkspaceFolder {
	return {
		uri: vscode.Uri.file(fsPath),
		name,
		index: 0,
	};
}

const token: vscode.CancellationToken = {
	isCancellationRequested: false,
	onCancellationRequested: (() => ({
		dispose: () => {},
		// eslint-disable-next-line @typescript-eslint/no-explicit-any
	})) as any as vscode.CancellationToken["onCancellationRequested"],
};

describe("GafferMcpProvider", () => {
	beforeEach(() => {
		resetVscode();
		setTrusted(true);
		setWorkspaceFolders([makeFolder("proj", "/ws/proj")]);
	});
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it("returns [] when the workspace is untrusted", () => {
		setTrusted(false);
		const provider = new GafferMcpProvider();
		expect(provider.provideMcpServerDefinitions(token)).toEqual([]);
	});

	it("returns [] when no workspace folders are open", () => {
		setWorkspaceFolders([]);
		const provider = new GafferMcpProvider();
		expect(provider.provideMcpServerDefinitions(token)).toEqual([]);
	});

	it("returns one definition per workspace folder", () => {
		setWorkspaceFolders([makeFolder("a", "/ws/a"), makeFolder("b", "/ws/b")]);
		const provider = new GafferMcpProvider();
		const defs = provider.provideMcpServerDefinitions(token);
		expect(defs).toHaveLength(2);
	});

	it("uses the folder uri as cwd", () => {
		const provider = new GafferMcpProvider();
		const defs = provider.provideMcpServerDefinitions(token);
		expect(defs[0]?.cwd).toBeInstanceOf(vscode.Uri);
		expect(defs[0]?.cwd?.fsPath).toBe("/ws/proj");
	});

	it("constructs definitions with empty env and undefined version", () => {
		const provider = new GafferMcpProvider();
		const defs = provider.provideMcpServerDefinitions(token);
		expect(defs[0]?.env).toEqual({});
		expect(defs[0]?.version).toBeUndefined();
	});

	it("labels single-folder workspaces 'Gaffer'", () => {
		const provider = new GafferMcpProvider();
		const defs = provider.provideMcpServerDefinitions(token);
		expect(defs[0]?.label).toBe("Gaffer");
	});

	it("disambiguates labels in multi-root workspaces", () => {
		setWorkspaceFolders([
			makeFolder("alpha", "/ws/a"),
			makeFolder("beta", "/ws/b"),
		]);
		const provider = new GafferMcpProvider();
		const defs = provider.provideMcpServerDefinitions(token);
		expect(defs.map((d) => d.label)).toEqual([
			"Gaffer (alpha)",
			"Gaffer (beta)",
		]);
	});

	it("invokes 'gaffer mcp' from the User-scoped gaffer.command", () => {
		setConfiguration("gaffer", "command", {
			globalValue: ["/usr/local/bin/gaffer", "--quiet"],
		});
		const provider = new GafferMcpProvider();
		const defs = provider.provideMcpServerDefinitions(token);
		expect(defs[0]?.command).toBe("/usr/local/bin/gaffer");
		expect(defs[0]?.args).toEqual(["--quiet", "mcp"]);
	});

	it("fires onDidChangeMcpServerDefinitions on refresh()", () => {
		const provider = new GafferMcpProvider();
		const listener = vi.fn();
		provider.onDidChangeMcpServerDefinitions(listener);
		provider.refresh();
		expect(listener).toHaveBeenCalledTimes(1);
	});

	it("stops firing after the listener-disposable is disposed", () => {
		const provider = new GafferMcpProvider();
		const listener = vi.fn();
		const disp = provider.onDidChangeMcpServerDefinitions(listener);
		disp.dispose();
		provider.refresh();
		expect(listener).not.toHaveBeenCalled();
	});

	it("stops firing after dispose()", () => {
		const provider = new GafferMcpProvider();
		const listener = vi.fn();
		provider.onDidChangeMcpServerDefinitions(listener);
		provider.dispose();
		provider.refresh();
		expect(listener).not.toHaveBeenCalled();
	});
});
