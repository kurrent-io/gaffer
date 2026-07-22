import { afterEach, describe, expect, it } from "vitest";
import * as vscode from "vscode";
import {
	anyLoading,
	buildActionItems,
	preserveActiveItems,
	projectionActions,
	type ActionMenu,
	type ProjectionAction,
	type ProjectionActionsDeps,
	type ProjectionActionsEnv,
} from "./projection-actions.js";
import { getState, queueQuickPick } from "../../test/testutil/vscode-state.js";

afterEach(() => {
	getState().quickPickResolutions.length = 0;
	getState().quickPickCalls.length = 0;
});

const tomlUri = vscode.Uri.parse("file:///p/gaffer.toml");

type DiffArgs = Parameters<ProjectionActionsDeps["diff"]>[0];
type OperateArgs = Parameters<ProjectionActionsDeps["operate"]>[0];
type MenuArgs = Parameters<ActionMenu>[0];

// The stub menu resolves to the pick queued via queueQuickPick, so the dispatch
// tests read the same as before while the real createQuickPick presenter (whose
// repaint logic is covered via anyLoading / preserveActiveItems) is bypassed.
function capture(): {
	diffCalls: DiffArgs[];
	operateCalls: OperateArgs[];
	menuCalls: MenuArgs[];
	deps: ProjectionActionsDeps;
} {
	const diffCalls: DiffArgs[] = [];
	const operateCalls: OperateArgs[] = [];
	const menuCalls: MenuArgs[] = [];
	return {
		diffCalls,
		operateCalls,
		menuCalls,
		deps: {
			diff: (a) => {
				diffCalls.push(a);
				return Promise.resolve();
			},
			operate: (a) => {
				operateCalls.push(a);
				return Promise.resolve();
			},
			menu: (a) => {
				menuCalls.push(a);
				const item = getState().quickPickResolutions.shift() as
					| { pick?: ProjectionAction }
					| undefined;
				return Promise.resolve(item?.pick);
			},
		},
	};
}

function actionLabels(items: ReturnType<typeof buildActionItems>): string[] {
	return items.filter((i) => i.pick).map((i) => i.label);
}

describe("buildActionItems", () => {
	it("groups by env with a separator per env, in config order, default labelled", () => {
		const envs: ProjectionActionsEnv[] = [
			{ name: "local", default: false },
			{ name: "prod", default: true },
		];
		const separators = buildActionItems(envs)
			.filter((i) => i.kind === vscode.QuickPickItemKind.Separator)
			.map((i) => i.label);
		// Config order preserved; the default is labelled, not moved to the top.
		expect(separators).toEqual(["local", "prod (default)"]);
	});

	it("offers deploy + diff + pause/resume (unknown state) + a single delete", () => {
		const items = buildActionItems([{ name: "prod", default: true }]);
		expect(actionLabels(items)).toEqual([
			"$(rocket) Deploy",
			"$(history) History",
			"$(diff-single) Diff against deployed",
			"$(debug-pause) Pause",
			"$(debug-start) Resume",
			"$(trash) Delete",
		]);
	});

	it("offers pause + abort (not resume) when running", () => {
		const items = buildActionItems([
			{ name: "prod", default: true, state: "running" },
		]);
		expect(actionLabels(items)).toEqual([
			"$(rocket) Deploy",
			"$(history) History",
			"$(diff-single) Diff against deployed",
			"$(debug-pause) Pause",
			"$(debug-stop) Abort",
			"$(trash) Delete",
		]);
	});

	it("offers resume only (not pause/abort) when stopped", () => {
		const items = buildActionItems([
			{ name: "prod", default: true, state: "stopped" },
		]);
		expect(actionLabels(items)).toEqual([
			"$(rocket) Deploy",
			"$(history) History",
			"$(diff-single) Diff against deployed",
			"$(debug-start) Resume",
			"$(trash) Delete",
		]);
	});

	it("offers both pause and resume for a raw unknown state", () => {
		// The server normalises unknown to "", but a stray "unknown" must not be
		// read as a known non-running state (which would hide Pause).
		const items = buildActionItems([
			{ name: "prod", default: true, state: "unknown" },
		]);
		expect(actionLabels(items)).toEqual([
			"$(rocket) Deploy",
			"$(history) History",
			"$(diff-single) Diff against deployed",
			"$(debug-pause) Pause",
			"$(debug-start) Resume",
			"$(trash) Delete",
		]);
	});

	it("carries production on the operate picks and emits on delete", () => {
		const items = buildActionItems([
			{
				name: "prod",
				default: true,
				state: "running",
				production: true,
				emits: true,
			},
		]);
		expect(items.find((i) => i.label === "$(debug-pause) Pause")?.pick).toEqual(
			{ env: "prod", action: "pause", production: true },
		);
		expect(items.find((i) => i.label === "$(trash) Delete")?.pick).toEqual({
			env: "prod",
			action: "delete",
			production: true,
			emits: true,
		});
	});

	it("collapses a sign-in-needed env to a single Sign in, labelled", () => {
		const items = buildActionItems([
			{ name: "kc", default: false, status: "auth" },
		]);
		expect(
			items.find((i) => i.kind === vscode.QuickPickItemKind.Separator)?.label,
		).toBe("kc · sign-in needed");
		expect(actionLabels(items)).toEqual(["$(key) Sign in"]);
	});

	it("collapses an unavailable env to a single non-actionable notice", () => {
		const items = buildActionItems([
			{ name: "local", default: true, status: "unavailable" },
		]);
		// The row states the status, so the separator carries no note that repeats it.
		expect(
			items.find((i) => i.kind === vscode.QuickPickItemKind.Separator)?.label,
		).toBe("local (default)");
		// A single non-actionable notice (no pick, so it's filtered from actionLabels).
		const rows = items.filter(
			(i) => i.kind !== vscode.QuickPickItemKind.Separator,
		);
		expect(rows.map((i) => i.label)).toEqual(["$(warning) Unavailable"]);
		expect(rows[0]?.pick).toBeUndefined();
		expect(actionLabels(items)).toEqual([]);
	});

	it("collapses a still-loading env to a single non-actionable spinner row", () => {
		const items = buildActionItems([
			{ name: "prod", default: true, loading: true },
		]);
		const rows = items.filter(
			(i) => i.kind !== vscode.QuickPickItemKind.Separator,
		);
		expect(rows.map((i) => i.label)).toEqual(["$(sync~spin) Loading status…"]);
		expect(rows[0]?.pick).toBeUndefined();
		expect(actionLabels(items)).toEqual([]);
	});
});

describe("projectionActions", () => {
	it("runs the diff for the picked env", async () => {
		const { diffCalls, deps } = capture();
		queueQuickPick({ pick: { env: "prod", action: "diff" } });
		await projectionActions(deps)({
			name: "checkout",
			tomlUri,
			envs: [{ name: "prod", default: true }],
		});
		expect(diffCalls).toEqual([{ name: "checkout", tomlUri, env: "prod" }]);
	});

	it("routes a deploy pick to gaffer.deployPreview scoped to the projection", async () => {
		const { deps } = capture();
		getState().executeCommandCalls.length = 0;
		queueQuickPick({ pick: { env: "prod", action: "deploy" } });
		await projectionActions(deps)({
			name: "checkout",
			tomlUri,
			envs: [{ name: "prod", default: true }],
		});
		const calls = getState().executeCommandCalls.filter(
			(c) => c.name === "gaffer.deployPreview",
		);
		expect(calls).toHaveLength(1);
		expect(calls[0]?.args[0]).toEqual({
			name: "checkout",
			env: "prod",
			tomlUri,
		});
	});

	it("routes a history pick to gaffer.history with the confirm-tier production", async () => {
		const { deps } = capture();
		getState().executeCommandCalls.length = 0;
		queueQuickPick({
			pick: { env: "prod", action: "history", production: true },
		});
		await projectionActions(deps)({
			name: "checkout",
			tomlUri,
			envs: [{ name: "prod", default: true, production: true }],
		});
		const calls = getState().executeCommandCalls.filter(
			(c) => c.name === "gaffer.history",
		);
		expect(calls).toHaveLength(1);
		expect(calls[0]?.args[0]).toEqual({
			name: "checkout",
			env: "prod",
			tomlUri,
			production: true,
		});
	});

	it("routes a delete with production + emits", async () => {
		const { operateCalls, deps } = capture();
		queueQuickPick({
			pick: {
				env: "prod",
				action: "delete",
				production: true,
				emits: true,
			},
		});
		await projectionActions(deps)({
			name: "checkout",
			tomlUri,
			envs: [{ name: "prod", default: true }],
		});
		expect(operateCalls).toEqual([
			{
				name: "checkout",
				tomlUri,
				env: "prod",
				verb: "delete",
				production: true,
				emits: true,
			},
		]);
	});

	it("routes a sign-in pick to the gaffer.signIn command", async () => {
		const { deps } = capture();
		getState().executeCommandCalls.length = 0;
		queueQuickPick({ pick: { env: "kc", action: "signin" } });
		await projectionActions(deps)({
			name: "checkout",
			tomlUri,
			envs: [{ name: "kc", default: false, status: "auth" }],
		});
		const calls = getState().executeCommandCalls.filter(
			(c) => c.name === "gaffer.signIn",
		);
		expect(calls).toHaveLength(1);
		expect(calls[0]?.args[0]).toMatchObject({ env: "kc" });
	});

	it("passes production through as undefined when the pick omits it", async () => {
		const { operateCalls, deps } = capture();
		queueQuickPick({ pick: { env: "prod", action: "pause" } });
		await projectionActions(deps)({
			name: "checkout",
			tomlUri,
			envs: [{ name: "prod", default: true }],
		});
		expect(operateCalls[0]).toMatchObject({ verb: "pause" });
		expect(operateCalls[0]?.production).toBeUndefined();
	});

	it("does nothing (no menu) when there are no envs", async () => {
		const { diffCalls, operateCalls, menuCalls, deps } = capture();
		await projectionActions(deps)({ name: "checkout", tomlUri, envs: [] });
		expect(diffCalls).toEqual([]);
		expect(operateCalls).toEqual([]);
		expect(menuCalls).toHaveLength(0);
	});

	it("does nothing when the pick is dismissed", async () => {
		const { diffCalls, operateCalls, deps } = capture();
		queueQuickPick(undefined);
		await projectionActions(deps)({
			name: "checkout",
			tomlUri,
			envs: [{ name: "prod", default: true }],
		});
		expect(diffCalls).toEqual([]);
		expect(operateCalls).toEqual([]);
	});

	it("does nothing when the unavailable notice is picked", async () => {
		const { diffCalls, operateCalls, deps } = capture();
		getState().executeCommandCalls.length = 0;
		queueQuickPick({ label: "$(warning) Unavailable" });
		await projectionActions(deps)({
			name: "checkout",
			tomlUri,
			envs: [{ name: "local", default: true, status: "unavailable" }],
		});
		expect(diffCalls).toEqual([]);
		expect(operateCalls).toEqual([]);
		expect(getState().executeCommandCalls).toHaveLength(0);
	});

	it("ignores a picked separator row (no pick payload)", async () => {
		const { diffCalls, operateCalls, deps } = capture();
		queueQuickPick({ label: "prod", kind: vscode.QuickPickItemKind.Separator });
		await projectionActions(deps)({
			name: "checkout",
			tomlUri,
			envs: [{ name: "prod", default: true }],
		});
		expect(diffCalls).toEqual([]);
		expect(operateCalls).toEqual([]);
	});

	it("passes the initial envs and a live-source subscribe to the menu", async () => {
		const { menuCalls, deps } = capture();
		const watchCalls: { name: string; tomlUri: vscode.Uri }[] = [];
		let pushed: ((envs: ProjectionActionsEnv[]) => void) | undefined;
		deps.watchActions = (name, uri, onUpdate) => {
			watchCalls.push({ name, tomlUri: uri });
			pushed = onUpdate;
			return { dispose: () => {} };
		};
		const initial: ProjectionActionsEnv[] = [
			{ name: "prod", default: true, loading: true },
		];
		queueQuickPick(undefined);
		await projectionActions(deps)({ name: "checkout", tomlUri, envs: initial });

		expect(menuCalls[0]?.initial).toEqual(initial);
		// Driving the menu's subscribe reaches watchActions with the projection id.
		const disposable = menuCalls[0]?.subscribe?.(() => {});
		expect(watchCalls).toEqual([{ name: "checkout", tomlUri }]);
		expect(typeof pushed).toBe("function");
		expect(typeof disposable?.dispose).toBe("function");
	});

	it("omits subscribe when no live source is wired", async () => {
		const { menuCalls, deps } = capture();
		queueQuickPick(undefined);
		await projectionActions(deps)({
			name: "checkout",
			tomlUri,
			envs: [{ name: "prod", default: true }],
		});
		expect(menuCalls[0]?.subscribe).toBeUndefined();
	});
});

describe("anyLoading", () => {
	it("is true when any env's status is in flight", () => {
		expect(anyLoading([{ name: "a", default: true }])).toBe(false);
		expect(
			anyLoading([
				{ name: "a", default: true },
				{ name: "b", default: false, loading: true },
			]),
		).toBe(true);
	});
});

describe("preserveActiveItems", () => {
	it("keeps the previously-active action row, ignoring separators", () => {
		const items = buildActionItems([{ name: "prod", default: true }]);
		const deploy = items.find((i) => i.pick?.action === "deploy")?.pick;
		// undefined models a separator's (absent) pick - must not be restored.
		const kept = preserveActiveItems(items, [undefined, deploy]);
		expect(kept.map((i) => i.label)).toEqual(["$(rocket) Deploy"]);
	});

	it("restores the row in the right env when action labels collide", () => {
		const items = buildActionItems([
			{ name: "prod", default: true },
			{ name: "local", default: false },
		]);
		// local's Deploy carries the same label as prod's; only the pick's env differs.
		const localDeploy = items.find(
			(i) => i.pick?.env === "local" && i.pick?.action === "deploy",
		)?.pick;
		const kept = preserveActiveItems(items, [localDeploy]);
		expect(kept).toHaveLength(1);
		expect(kept[0]?.pick?.env).toBe("local");
	});

	it("returns nothing when no pick matches", () => {
		const items = buildActionItems([{ name: "prod", default: true }]);
		expect(
			preserveActiveItems(items, [
				{ env: "gone", action: "deploy", production: undefined },
			]),
		).toEqual([]);
	});
});
