import { afterEach, describe, expect, it } from "vitest";
import {
	clearServedMethods,
	METHOD_DIFF_PROJECTION,
	METHOD_OPERATE_PROJECTION,
	onServedMethodsChanged,
	readServedMethods,
	serverServes,
	setServedMethods,
} from "./capabilities.js";

afterEach(() => {
	clearServedMethods();
});

describe("readServedMethods", () => {
	it("reads the advertised methods out of the initialize result", () => {
		const methods = readServedMethods({
			capabilities: {
				experimental: {
					gaffer: {
						methods: [METHOD_DIFF_PROJECTION, METHOD_OPERATE_PROJECTION],
					},
				},
			},
		});
		expect([...methods].sort()).toEqual(
			[METHOD_DIFF_PROJECTION, METHOD_OPERATE_PROJECTION].sort(),
		);
	});

	// The case this whole mechanism exists for: a gaffer released before the
	// capability landed. It must read as "serves nothing", not as an error.
	it("returns an empty set for a server that advertises no experimental block", () => {
		expect(
			readServedMethods({
				capabilities: { codeLensProvider: {}, textDocumentSync: {} },
			}).size,
		).toBe(0);
	});

	it("returns an empty set when experimental is explicitly null", () => {
		expect(
			readServedMethods({ capabilities: { experimental: null } }).size,
		).toBe(0);
	});

	it("returns an empty set when the gaffer block carries no methods", () => {
		expect(
			readServedMethods({
				capabilities: { experimental: { gaffer: {} } },
			}).size,
		).toBe(0);
	});

	// Fails closed: a shape we can't read means we don't know what's served, so
	// callers must treat it the same as "serves nothing" rather than assuming.
	it("returns an empty set for a malformed methods list", () => {
		expect(
			readServedMethods({
				capabilities: { experimental: { gaffer: { methods: "diff" } } },
			}).size,
		).toBe(0);
	});

	it("returns an empty set for a non-object result", () => {
		expect(readServedMethods(undefined).size).toBe(0);
		expect(readServedMethods(null).size).toBe(0);
		expect(readServedMethods("nope").size).toBe(0);
	});

	// Unknown names are kept rather than filtered against a known list: the
	// server is authoritative about what it serves, and a newer CLI may advertise
	// methods this extension has no gate for yet.
	it("keeps method names it has no gate for", () => {
		const methods = readServedMethods({
			capabilities: {
				experimental: { gaffer: { methods: ["gaffer/somethingNewer"] } },
			},
		});
		expect(methods.has("gaffer/somethingNewer")).toBe(true);
	});
});

describe("serverServes", () => {
	it("is false before any server has started", () => {
		expect(serverServes(METHOD_DIFF_PROJECTION)).toBe(false);
	});

	it("is true for an advertised method and false for one that isn't", () => {
		setServedMethods(new Set([METHOD_DIFF_PROJECTION]));
		expect(serverServes(METHOD_DIFF_PROJECTION)).toBe(true);
		expect(serverServes(METHOD_OPERATE_PROJECTION)).toBe(false);
	});

	// A restart can land on a different binary if the user updated the CLI
	// mid-session, so the set is replaced rather than accumulated.
	it("replaces the previous set on a restart rather than merging", () => {
		setServedMethods(new Set([METHOD_DIFF_PROJECTION]));
		setServedMethods(new Set([METHOD_OPERATE_PROJECTION]));
		expect(serverServes(METHOD_DIFF_PROJECTION)).toBe(false);
		expect(serverServes(METHOD_OPERATE_PROJECTION)).toBe(true);
	});

	it("is false for everything once cleared", () => {
		setServedMethods(new Set([METHOD_DIFF_PROJECTION]));
		clearServedMethods();
		expect(serverServes(METHOD_DIFF_PROJECTION)).toBe(false);
	});
});

// The gated surfaces are painted from cached state - CodeLenses and an open
// action menu - so a change has to push a repaint. Without one, a mid-session CLI
// swap leaves a downgraded server offering actions that now fail on click, and an
// upgraded one hiding actions it could serve, until something unrelated repaints.
describe("onServedMethodsChanged", () => {
	it("fires when the set gains a method", () => {
		let fired = 0;
		const sub = onServedMethodsChanged(() => fired++);
		setServedMethods(new Set([METHOD_DIFF_PROJECTION]));
		expect(fired).toBe(1);
		sub.dispose();
	});

	it("fires when the set loses a method", () => {
		setServedMethods(new Set([METHOD_DIFF_PROJECTION]));
		let fired = 0;
		const sub = onServedMethodsChanged(() => fired++);
		setServedMethods(new Set());
		expect(fired).toBe(1);
		sub.dispose();
	});

	it("fires on clear", () => {
		setServedMethods(new Set([METHOD_DIFF_PROJECTION]));
		let fired = 0;
		const sub = onServedMethodsChanged(() => fired++);
		clearServedMethods();
		expect(fired).toBe(1);
		sub.dispose();
	});

	// The same server's set is written more than once - the state hook and the
	// post-start read - and each redundant write would otherwise cost a full lens
	// repaint.
	it("doesn't fire when the same set is written again", () => {
		setServedMethods(new Set([METHOD_DIFF_PROJECTION]));
		let fired = 0;
		const sub = onServedMethodsChanged(() => fired++);
		setServedMethods(new Set([METHOD_DIFF_PROJECTION]));
		expect(fired).toBe(0);
		sub.dispose();
	});

	// Same size, different members: a size-only comparison would miss a swap of one
	// capability for another.
	it("fires when a same-sized set has different members", () => {
		setServedMethods(new Set([METHOD_DIFF_PROJECTION]));
		let fired = 0;
		const sub = onServedMethodsChanged(() => fired++);
		setServedMethods(new Set([METHOD_OPERATE_PROJECTION]));
		expect(fired).toBe(1);
		sub.dispose();
	});

	it("stops firing once disposed", () => {
		let fired = 0;
		const sub = onServedMethodsChanged(() => fired++);
		sub.dispose();
		setServedMethods(new Set([METHOD_DIFF_PROJECTION]));
		expect(fired).toBe(0);
	});
});
