import * as vscode from "vscode";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { StatusPoller } from "./status-poller.js";
import { makeFakeTextEditor } from "../../test/__mocks__/vscode.js";
import {
	fireVisibleEditorsChanged,
	resetVscode,
	setVisibleTextEditors,
} from "../../test/testutil/vscode-state.js";

const INTERVAL = 5_000;

function editor(
	uri: string,
	opts: { dirty?: boolean } = {},
): ReturnType<typeof makeFakeTextEditor> {
	return makeFakeTextEditor({
		uri: vscode.Uri.parse(uri),
		isDirty: opts.dirty ?? false,
	} as vscode.TextDocument);
}

// The refresh callback records uri.toString(); compare against the same
// round-trip so the test is agnostic to how Uri normalizes its string form.
function k(uri: string): string {
	return vscode.Uri.parse(uri).toString();
}

describe("StatusPoller", () => {
	beforeEach(() => {
		resetVscode();
		vi.useFakeTimers();
	});
	afterEach(() => {
		vi.useRealTimers();
	});

	it("polls each visible gaffer.toml on the interval", () => {
		setVisibleTextEditors([editor("file:///ws/gaffer.toml")]);
		const refreshed: string[] = [];
		const poller = new StatusPoller(
			(uri) => refreshed.push(uri.toString()),
			INTERVAL,
		);

		expect(refreshed).toEqual([]); // no immediate poll
		vi.advanceTimersByTime(INTERVAL);
		vi.advanceTimersByTime(INTERVAL);
		expect(refreshed).toEqual([
			k("file:///ws/gaffer.toml"),
			k("file:///ws/gaffer.toml"),
		]);

		poller.dispose();
	});

	it("ignores editors that are not a gaffer.toml", () => {
		setVisibleTextEditors([
			editor("file:///ws/projection.js"),
			editor("file:///ws/notgaffer.toml"),
		]);
		const refreshed: string[] = [];
		const poller = new StatusPoller(
			(uri) => refreshed.push(uri.toString()),
			INTERVAL,
		);

		vi.advanceTimersByTime(INTERVAL);
		expect(refreshed).toEqual([]);

		poller.dispose();
	});

	it("refreshes a config shown in a split only once per tick", () => {
		setVisibleTextEditors([
			editor("file:///ws/gaffer.toml"),
			editor("file:///ws/gaffer.toml"),
			editor("file:///other/gaffer.toml"),
		]);
		const refreshed: string[] = [];
		const poller = new StatusPoller(
			(uri) => refreshed.push(uri.toString()),
			INTERVAL,
		);

		vi.advanceTimersByTime(INTERVAL);
		expect(refreshed.sort()).toEqual(
			[k("file:///other/gaffer.toml"), k("file:///ws/gaffer.toml")].sort(),
		);

		poller.dispose();
	});

	it("skips a config with unsaved edits", () => {
		setVisibleTextEditors([editor("file:///ws/gaffer.toml", { dirty: true })]);
		const refreshed: string[] = [];
		const poller = new StatusPoller(
			(uri) => refreshed.push(uri.toString()),
			INTERVAL,
		);

		vi.advanceTimersByTime(INTERVAL * 2);
		expect(refreshed).toEqual([]); // dirty buffer could be mid-edit - don't poll it

		poller.dispose();
	});

	it("polls a saved config but skips a dirty one shown alongside it", () => {
		setVisibleTextEditors([
			editor("file:///ws/gaffer.toml", { dirty: false }),
			editor("file:///other/gaffer.toml", { dirty: true }),
		]);
		const refreshed: string[] = [];
		const poller = new StatusPoller(
			(uri) => refreshed.push(uri.toString()),
			INTERVAL,
		);

		vi.advanceTimersByTime(INTERVAL);
		expect(refreshed).toEqual([k("file:///ws/gaffer.toml")]);

		poller.dispose();
	});

	it("does not run a timer until a config is visible", () => {
		setVisibleTextEditors([editor("file:///ws/projection.js")]);
		const refreshed: string[] = [];
		const poller = new StatusPoller(
			(uri) => refreshed.push(uri.toString()),
			INTERVAL,
		);

		vi.advanceTimersByTime(INTERVAL * 3);
		expect(refreshed).toEqual([]); // idle: no timer

		// A gaffer.toml becomes visible - the poller starts polling it.
		fireVisibleEditorsChanged([editor("file:///ws/gaffer.toml")]);
		vi.advanceTimersByTime(INTERVAL);
		expect(refreshed).toEqual([k("file:///ws/gaffer.toml")]);

		poller.dispose();
	});

	it("stops polling once the last config is hidden", () => {
		setVisibleTextEditors([editor("file:///ws/gaffer.toml")]);
		const refreshed: string[] = [];
		const poller = new StatusPoller(
			(uri) => refreshed.push(uri.toString()),
			INTERVAL,
		);

		vi.advanceTimersByTime(INTERVAL);
		expect(refreshed).toHaveLength(1);

		fireVisibleEditorsChanged([]); // config closed
		vi.advanceTimersByTime(INTERVAL * 3);
		expect(refreshed).toHaveLength(1); // no further polls

		poller.dispose();
	});

	it("stops polling after dispose", () => {
		setVisibleTextEditors([editor("file:///ws/gaffer.toml")]);
		const refreshed: string[] = [];
		const poller = new StatusPoller(
			(uri) => refreshed.push(uri.toString()),
			INTERVAL,
		);

		vi.advanceTimersByTime(INTERVAL);
		expect(refreshed).toHaveLength(1);

		poller.dispose();
		vi.advanceTimersByTime(INTERVAL * 3);
		expect(refreshed).toHaveLength(1);
	});
});
