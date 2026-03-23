import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { join } from "path";
import { ProjectionSession } from "../src/index.js";
import type { EmittedEvent } from "../src/index.js";

interface Fixture {
	name: string;
	source: string;
	options?: Record<string, unknown>;
	setState?: { partition: string | null; state: string };
	events?: Record<string, unknown>[];
	expect: {
		valid?: boolean;
		sources?: Record<string, unknown>;
		state?: unknown;
		states?: Record<string, unknown>;
		sharedState?: unknown;
		result?: unknown;
		emitted?: { streamId: string; eventType: string; data: string }[];
		logs?: string[];
		error?: string;
	};
}

function loadFixtures(filename: string): Fixture[] {
	const path = join(__dirname, "..", "..", "..", "tools", "fixtures", filename);
	return JSON.parse(readFileSync(path, "utf8"));
}

function runFixtures(filename: string) {
	const fixtures = loadFixtures(filename);
	for (const f of fixtures) {
		it(f.name, () => runFixture(f));
	}
}

function runFixture(f: Fixture) {
	const optionsJson = f.options ? JSON.stringify(f.options) : undefined;

	// Check validity
	if (f.expect.valid === false) {
		expect(
			() =>
				new ProjectionSession(
					f.source,
					optionsJson ? JSON.parse(optionsJson) : undefined,
				),
		).toThrow();
		return;
	}

	const session = new ProjectionSession(
		f.source,
		optionsJson ? JSON.parse(optionsJson) : undefined,
	);

	try {
		// Check sources
		if (f.expect.sources) {
			const sources = session.getSources();
			for (const [key, expected] of Object.entries(f.expect.sources)) {
				expect((sources as unknown as Record<string, unknown>)[key]).toEqual(
					expected,
				);
			}
		}

		// Set state
		if (f.setState) {
			session.setState(f.setState.partition, f.setState.state);
		}

		if (!f.events?.length) return;

		// Feed events
		let lastError: string | null = null;
		let lastEmitted: EmittedEvent[] = [];
		let lastLogs: string[] = [];

		session.onEmit((event) => {
			lastEmitted.push(event);
		});
		session.onLog((message) => {
			lastLogs.push(message);
		});

		for (const event of f.events) {
			lastEmitted = [];
			lastLogs = [];

			try {
				// eslint-disable-next-line @typescript-eslint/no-explicit-any
				session.feed(event as any);
			} catch (err) {
				lastError = err instanceof Error ? err.message : String(err);
			}
		}

		// Check error
		if (f.expect.error) {
			expect(lastError).not.toBeNull();
			expect(lastError).toContain(f.expect.error);
			return;
		}

		// Check state
		if (f.expect.state !== undefined) {
			const state = session.getStateJson();
			expect(state).toEqual(f.expect.state);
		}

		// Check per-partition states
		if (f.expect.states) {
			for (const [partition, expected] of Object.entries(f.expect.states)) {
				if (expected === null) {
					expect(session.getState(partition)).toBeNull();
				} else {
					const state = session.getStateJson(partition);
					expect(state).toEqual(expected);
				}
			}
		}

		// Check shared state
		if (f.expect.sharedState !== undefined) {
			expect(session.getSharedStateJson()).toEqual(f.expect.sharedState);
		}

		// Check result
		if (f.expect.result !== undefined) {
			if (f.expect.result === null) {
				expect(session.getResult()).toBeNull();
			} else {
				expect(session.getResultJson()).toEqual(f.expect.result);
			}
		}

		// Check emitted
		if (f.expect.emitted !== undefined) {
			expect(lastEmitted).toHaveLength(f.expect.emitted.length);
			for (let i = 0; i < f.expect.emitted.length; i++) {
				expect(lastEmitted[i].streamId).toBe(f.expect.emitted[i].streamId);
				expect(lastEmitted[i].eventType).toBe(f.expect.emitted[i].eventType);
				if (f.expect.emitted[i].data) {
					expect(lastEmitted[i].data).toBe(f.expect.emitted[i].data);
				}
			}
		}

		// Check logs
		if (f.expect.logs !== undefined) {
			expect(lastLogs).toEqual(f.expect.logs);
		}
	} finally {
		session.dispose();
	}
}

describe("Fixtures: sources", () => runFixtures("sources.json"));
describe("Fixtures: state", () => runFixtures("state.json"));
describe("Fixtures: callbacks", () => runFixtures("callbacks.json"));
describe("Fixtures: errors", () => runFixtures("errors.json"));
describe("Fixtures: transforms", () => runFixtures("transforms.json"));
describe("Fixtures: deletion", () => runFixtures("deletion.json"));
describe("Fixtures: versioning", () => runFixtures("versioning.json"));
