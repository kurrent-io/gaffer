import { ProjectionSession } from "@kurrent/gaffer-runtime";
import { KurrentDBClient } from "@kurrent/kurrentdb-client";
import {
	buildSubscriptionFilter,
	getResolveLinks,
} from "./subscriptionFilter.js";
import { mapQuerySources, type ProjectionInfo } from "./ProjectionInfo.js";
import {
	ProjectionTest,
	toSessionOptions,
	type ProjectionOptions,
	type StepResult,
} from "./ProjectionTest.js";
import type { EventInput } from "./schemas.js";

/**
 * A projection that can be validated, run against events, or tested interactively.
 * Created via {@link createProjection}.
 */
export interface Projection<
	TState = unknown,
	TResult = TState,
	TSharedState = undefined,
> {
	/**
	 * Compile the projection and return its source definition.
	 * @throws {ProjectionError} If the projection source is invalid.
	 */
	validate(): ProjectionInfo;
	/**
	 * Run the projection over a sync iterable of events.
	 * @throws {ProjectionError} If the projection source is invalid or a handler throws.
	 */
	run(
		events: Iterable<EventInput>,
	): Iterable<StepResult<TState, TResult, TSharedState>>;
	/**
	 * Run the projection over an async iterable of events.
	 * @throws {ProjectionError} If the projection source is invalid or a handler throws.
	 */
	run(
		events: AsyncIterable<EventInput>,
	): AsyncIterable<StepResult<TState, TResult, TSharedState>>;
	/**
	 * Run the projection against a live KurrentDB subscription.
	 * @throws {ProjectionError} If the projection source is invalid or a handler throws.
	 */
	run(
		client: KurrentDBClient,
	): AsyncIterable<StepResult<TState, TResult, TSharedState>>;
	/**
	 * Create an interactive test session for feeding events one at a time.
	 * @throws {InvalidProjectionError} If the projection source is invalid.
	 */
	test(): ProjectionTest<TState, TResult, TSharedState>;
}

/**
 * Create a projection from JavaScript source code.
 * Does not compile until {@link Projection.validate}, {@link Projection.run},
 * or {@link Projection.test} is called.
 */
export function createProjection<
	TState = unknown,
	TResult = TState,
	TSharedState = undefined,
>(
	/** KurrentDB projection JavaScript source code. */
	source: string,
	/** Options for the projection session (version, timeouts, etc). */
	options?: ProjectionOptions,
): Projection<TState, TResult, TSharedState> {
	return {
		validate(): ProjectionInfo {
			let session: ProjectionSession | null = null;
			try {
				session = new ProjectionSession(source, toSessionOptions(options));
				return mapQuerySources(session.getSources());
			} finally {
				session?.dispose();
			}
		},

		run(input: unknown) {
			const info = this.validate();

			if (input instanceof KurrentDBClient) {
				return runWithClient<TState, TResult, TSharedState>(
					source,
					options,
					input,
					info,
				);
			}

			if (isAsyncIterable(input)) {
				return runAsync<TState, TResult, TSharedState>(source, options, input);
			}

			if (isIterable(input)) {
				return runSync<TState, TResult, TSharedState>(source, options, input);
			}

			throw new Error(
				"run() expects an Iterable, AsyncIterable, or KurrentDBClient",
			);
		},

		test(): ProjectionTest<TState, TResult, TSharedState> {
			return new ProjectionTest<TState, TResult, TSharedState>(source, options);
		},
	} as Projection<TState, TResult, TSharedState>;
}

function* runSync<TState, TResult, TSharedState>(
	source: string,
	options: ProjectionOptions | undefined,
	events: Iterable<EventInput>,
): Iterable<StepResult<TState, TResult, TSharedState>> {
	const test = new ProjectionTest<TState, TResult, TSharedState>(
		source,
		options,
	);
	try {
		for (const event of events) {
			yield test.feed(event);
		}
	} finally {
		test.dispose();
	}
}

async function* runAsync<TState, TResult, TSharedState>(
	source: string,
	options: ProjectionOptions | undefined,
	events: AsyncIterable<EventInput>,
): AsyncIterable<StepResult<TState, TResult, TSharedState>> {
	const test = new ProjectionTest<TState, TResult, TSharedState>(
		source,
		options,
	);
	try {
		for await (const event of events) {
			yield test.feed(event);
		}
	} finally {
		test.dispose();
	}
}

async function* runWithClient<TState, TResult, TSharedState>(
	source: string,
	options: ProjectionOptions | undefined,
	client: KurrentDBClient,
	info: ProjectionInfo,
): AsyncIterable<StepResult<TState, TResult, TSharedState>> {
	const test = new ProjectionTest<TState, TResult, TSharedState>(
		source,
		options,
	);
	const subscription = createSubscription(client, info, options?.version);
	try {
		for await (const event of subscription) {
			yield test.feed(event);
		}
	} finally {
		test.dispose();
		subscription.unsubscribe();
	}
}

function createSubscription(
	client: KurrentDBClient,
	info: ProjectionInfo,
	version: "v1" | "v2" = "v2",
) {
	const filter = buildSubscriptionFilter(info);
	const resolveLinkTos = getResolveLinks(version);
	return client.subscribeToAll({ filter, resolveLinkTos });
}

const isIterable = (value: unknown): value is Iterable<EventInput> =>
	value !== null && typeof value === "object" && Symbol.iterator in value;

const isAsyncIterable = (value: unknown): value is AsyncIterable<EventInput> =>
	value !== null && typeof value === "object" && Symbol.asyncIterator in value;
