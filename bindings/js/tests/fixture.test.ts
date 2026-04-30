import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { ProjectionSession, ProjectionError } from "../src/index.js";
import type { EmittedEvent } from "../src/index.js";

interface FixtureError {
	code: string;
	description?: string;
}

import type { SessionOptions } from "../src/types.js";

interface Fixture {
	name: string;
	source: string;
	options?: SessionOptions;
	setState?: { partition: string | null; state: string };
	events?: Record<string, unknown>[];
	expect: {
		sources?: Record<string, unknown>;
		state?: unknown;
		states?: Record<string, unknown>;
		sharedState?: unknown;
		result?: unknown;
		emitted?: { streamId: string; eventType: string; data: string }[];
		logs?: string[];
		error?: FixtureError;
		getResult?: boolean;
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

function assertError(error: ProjectionError, expected: FixtureError) {
	expect(error.code).toBe(expected.code);
	if (expected.description) {
		expect(error.description).toContain(expected.description);
	}
}

function runFixture(f: Fixture) {
	if (!f.options) {
		throw new Error(
			`Fixture "${f.name}" missing required options (engineVersion)`,
		);
	}

	// Creation error (no events, no getResult)
	if (f.expect.error && !f.events?.length && !f.expect.getResult) {
		try {
			new ProjectionSession(f.source, f.options);
			expect.fail("Expected error but session created successfully");
		} catch (err) {
			expect(err).toBeInstanceOf(ProjectionError);
			assertError(err as ProjectionError, f.expect.error);
		}
		return;
	}

	const session = new ProjectionSession(f.source, f.options);

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

		// Feed events
		if (f.events?.length) {
			let lastFeedError: ProjectionError | null = null;
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
					if (err instanceof ProjectionError) {
						lastFeedError = err;
					} else {
						throw err;
					}
				}
			}

			// Check feed error (not getResult)
			if (f.expect.error && !f.expect.getResult) {
				expect(lastFeedError).not.toBeNull();
				if (!lastFeedError) return;
				assertError(lastFeedError, f.expect.error);
				return;
			}

			// Check emitted
			if (f.expect.emitted !== undefined) {
				expect(lastEmitted).toHaveLength(f.expect.emitted.length);
				for (const [i, expected] of f.expect.emitted.entries()) {
					const actual = lastEmitted[i];
					expect(actual).toBeDefined();
					if (!actual) continue;
					expect(actual.streamId).toBe(expected.streamId);
					expect(actual.eventType).toBe(expected.eventType);
					if (expected.data) {
						expect(actual.data).toBe(expected.data);
					}
				}
			}

			// Check logs
			if (f.expect.logs !== undefined) {
				expect(lastLogs).toEqual(f.expect.logs);
			}
		}

		// Check getResult error
		if (f.expect.getResult && f.expect.error) {
			try {
				session.getResult();
				expect.fail("Expected error but getResult succeeded");
			} catch (err) {
				expect(err).toBeInstanceOf(ProjectionError);
				assertError(err as ProjectionError, f.expect.error);
			}
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
