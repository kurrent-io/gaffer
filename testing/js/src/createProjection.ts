import { ProjectionSession } from "@kurrent/gaffer-runtime";
import {
	KurrentDBClient,
	streamNameFilter,
	eventTypeFilter,
} from "@kurrent/kurrentdb-client";
import { mapQuerySources, type ProjectionInfo } from "./ProjectionInfo.js";
import {
	ProjectionTest,
	InvalidProjectionError,
	type StepResult,
} from "./ProjectionTest.js";
import type { EventInput } from "./schemas.js";

/** Result of validating a projection's JavaScript source. */
export type ValidationResult =
	| { valid: true; info: ProjectionInfo }
	| { valid: false; error: string };

/**
 * A projection that can be validated, run against events, or tested interactively.
 * Created via {@link createProjection}.
 */
export interface Projection<TState = unknown> {
	/** Compile the projection and return its source definition, or an error. */
	validate(): ValidationResult;
	/**
	 * Run the projection over a sync iterable of events.
	 * @throws {InvalidProjectionError} If the projection source is invalid.
	 * @throws {ProjectionError} If a handler throws during event processing.
	 */
	run(events: Iterable<EventInput>): Iterable<StepResult<TState>>;
	/**
	 * Run the projection over an async iterable of events.
	 * @throws {InvalidProjectionError} If the projection source is invalid.
	 * @throws {ProjectionError} If a handler throws during event processing.
	 */
	run(events: AsyncIterable<EventInput>): AsyncIterable<StepResult<TState>>;
	/**
	 * Run the projection against a live KurrentDB subscription.
	 * @throws {InvalidProjectionError} If the projection source is invalid.
	 * @throws {ProjectionError} If a handler throws during event processing.
	 */
	run(client: KurrentDBClient): AsyncIterable<StepResult<TState>>;
	/**
	 * Create an interactive test session for feeding events one at a time.
	 * @throws {InvalidProjectionError} If the projection source is invalid.
	 */
	test(): ProjectionTest<TState>;
}

/**
 * Create a projection from JavaScript source code.
 * Does not compile until {@link Projection.validate}, {@link Projection.run},
 * or {@link Projection.test} is called.
 */
export function createProjection<TState = unknown>(
	/** KurrentDB projection JavaScript source code. */
	source: string,
): Projection<TState> {
	return {
		validate(): ValidationResult {
			let session: ProjectionSession | null = null;
			try {
				session = new ProjectionSession(source);
				const raw = session.getSources();
				return { valid: true, info: mapQuerySources(raw) };
			} catch (err) {
				return {
					valid: false,
					error: err instanceof Error ? err.message : String(err),
				};
			} finally {
				session?.dispose();
			}
		},

		run(input: unknown) {
			const validation = this.validate();

			if (!validation.valid) {
				throw new InvalidProjectionError(validation.error);
			}

			if (input instanceof KurrentDBClient) {
				return runWithClient<TState>(source, input, validation.info);
			}

			if (isAsyncIterable(input)) {
				return runAsync<TState>(source, input);
			}

			if (isIterable(input)) {
				return runSync<TState>(source, input);
			}

			throw new Error(
				"run() expects an Iterable, AsyncIterable, or KurrentDBClient",
			);
		},

		test(): ProjectionTest<TState> {
			return new ProjectionTest<TState>(source);
		},
	} as Projection<TState>;
}

function* runSync<TState>(
	source: string,
	events: Iterable<EventInput>,
): Iterable<StepResult<TState>> {
	const test = new ProjectionTest<TState>(source);
	try {
		for (const event of events) {
			yield test.feed(event);
		}
	} finally {
		test.dispose();
	}
}

async function* runAsync<TState>(
	source: string,
	events: AsyncIterable<EventInput>,
): AsyncIterable<StepResult<TState>> {
	const test = new ProjectionTest<TState>(source);
	try {
		for await (const event of events) {
			yield test.feed(event);
		}
	} finally {
		test.dispose();
	}
}

async function* runWithClient<TState>(
	source: string,
	client: KurrentDBClient,
	info: ProjectionInfo,
): AsyncIterable<StepResult<TState>> {
	const test = new ProjectionTest<TState>(source);
	const subscription = createSubscription(client, info);
	try {
		for await (const event of subscription) {
			yield test.feed(event);
		}
	} finally {
		test.dispose();
		subscription.unsubscribe();
	}
}

function createSubscription(client: KurrentDBClient, info: ProjectionInfo) {
	// The runtime handles event type filtering internally (matching real KurrentDB),
	// but for fromAll() we also filter at the subscription level to avoid transferring
	// irrelevant events over the wire. For streams/categories, the KurrentDB API only
	// supports a single filter so stream name takes priority.
	switch (info.source.type) {
		case "all":
			return client.subscribeToAll(
				info.events !== "all"
					? { filter: eventTypeFilter({ prefixes: info.events }) }
					: undefined,
			);
		case "streams":
			return client.subscribeToAll({
				filter: streamNameFilter({
					regex: `^(${info.source.streams.map(escapeRegex).join("|")})$`,
				}),
			});
		case "categories":
			return client.subscribeToAll({
				filter: streamNameFilter({
					prefixes: info.source.categories.map((c) => c + "-"),
				}),
			});
	}
}

const escapeRegex = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

const isIterable = (value: unknown): value is Iterable<EventInput> =>
	value !== null && typeof value === "object" && Symbol.iterator in value;

const isAsyncIterable = (value: unknown): value is AsyncIterable<EventInput> =>
	value !== null && typeof value === "object" && Symbol.asyncIterator in value;
