import {
	ProjectionSession,
	type EmittedEvent,
	type FeedResult,
	type SessionOptions,
} from "@kurrent/gaffer-runtime";
import { parseEventInput, normalizeEvent, type EventInput } from "./schemas.js";

/** An event emitted by a projection via `emit()` or `linkTo()`, with parsed data. */
export interface TestEmittedEvent {
	/** Target stream for the emitted event. */
	streamId: string;
	/** Event type name, or `$>` / `$@` for link events. */
	eventType: string;
	/** Parsed from JSON string. Falls back to raw string if parsing fails. */
	data: unknown;
	/** Parsed metadata object, or null if no metadata was provided. */
	metadata: Record<string, string | null> | null;
	/** True for `linkTo()` events (`$>` or `$@` event types). */
	isLink: boolean;
}

/** Result of feeding a single event to a projection. Discriminate on `status`. */
export type StepResult<
	TState = unknown,
	TResult = TState,
	TSharedState = undefined,
> = ProcessedStepResult<TState, TResult, TSharedState> | SkippedStepResult;

/** Result when the event was processed by the handler. */
export interface ProcessedStepResult<
	TState = unknown,
	TResult = TState,
	TSharedState = undefined,
> {
	status: "processed";
	/** Projection state for the affected partition. */
	state: TState;
	/** Result for the affected partition. V1: transformed state (after `transformBy`/`filterBy`), or state if no transform. V2: post-handler state (transforms aren't invoked). */
	result: TResult;
	/** Shared state for biState projections. */
	sharedState: TSharedState;
	/** Partition key that was updated. Absent for unpartitioned projections. */
	partition?: string;
	/** The input event that was fed. */
	event: EventInput;
	/** Events emitted during processing (emit/linkTo). */
	emitted: TestEmittedEvent[];
	/** Log messages from `log()` calls. */
	logs: string[];
}

/**
 * Why an event was skipped by the runtime.
 *
 * - `"non-json"` - V1 mode dropped the event because `isJson` is false.
 * - `"link"` - Event is a link (`$>` type or has `linkMetadata`) and `$includeLinks` is false.
 * - `"no-partition"` - The `partitionBy` function returned null, undefined, or a non-string/non-number value.
 * - `"unhandled"` - The event type is not in the projection's handler list and no `$any` handler is registered.
 * - `"no-delete-handler"` - A stream deletion event was received but no `$deleted` handler is registered.
 */
export type SkipReason =
	| "non-json"
	| "link"
	| "no-partition"
	| "unhandled"
	| "no-delete-handler";

/** Result when the event was filtered out before reaching the handler. */
export interface SkippedStepResult {
	status: "skipped";
	/** Why the event was skipped. */
	reason: SkipReason;
	/** The input event that was fed. */
	event: EventInput;
}

/** Per-projection configuration. */
export interface ProjectionConfig {
	/** Maximum time for JS handler execution per event in ms. Default: 5000. */
	executionTimeoutMs?: number;
}

/** Database-wide configuration. */
export interface DatabaseConfig {
	/** Maximum time for JS compilation in ms. Default: 5000. */
	compilationTimeoutMs?: number;
	/** Maximum time for JS handler execution per event in ms (default, overridden by projection config). Default: 5000. */
	executionTimeoutMs?: number;
}

/** Options for configuring a projection session. */
export interface ProjectionOptions {
	/** Projection engine version. Required. */
	engineVersion: 1 | 2;
	/**
	 * Target KurrentDB version (`MAJOR.MINOR.PATCH`, e.g. `"26.1.0"`).
	 * Unset means "unversioned": gaffer reproduces every known KurrentDB quirk.
	 * Set a version to turn off quirks that have been fixed upstream as of that
	 * version.
	 */
	quirksVersion?: string;
	/** Per-projection settings. */
	config?: ProjectionConfig;
	/** Database-wide settings. */
	databaseConfig?: DatabaseConfig;
}

const registry = new FinalizationRegistry<ProjectionSession>((session) => {
	session.dispose();
});

/**
 * Interactive test session for a projection. Feed events one at a time and
 * inspect state, emitted events, and logs after each step.
 *
 * Must be disposed when done to free native resources. Supports `using` syntax.
 */
export class ProjectionTest<
	TState = unknown,
	TResult = TState,
	TSharedState = undefined,
> {
	#session: ProjectionSession;
	#disposed = false;

	constructor(source: string, options: ProjectionOptions) {
		this.#session = new ProjectionSession(source, toSessionOptions(options));
		registry.register(this, this.#session, this);
	}

	/**
	 * Feed a single event to the projection. Returns the step result.
	 * @throws {ProjectionError} If the projection handler throws, with `input` and `normalized` fields attached.
	 */
	feed(
		/** A TestEvent, RecordedEvent, or ResolvedEvent. */
		input: EventInput,
	): StepResult<TState, TResult, TSharedState> {
		this.#ensureNotDisposed();

		const parsed = parseEventInput(input);
		const normalized = normalizeEvent(parsed);
		const feedResult = this.#session.feed(normalized);

		return mapStepResult<TState, TResult, TSharedState>(feedResult, input);
	}

	/** Get current state for a partition. Pass no argument for unpartitioned projections. */
	getState(
		/** Partition key. */
		partition?: string,
	): TState | null {
		this.#ensureNotDisposed();
		return this.#session.getStateJson<TState>(partition) ?? null;
	}

	/** Get shared state for biState projections. */
	getSharedState(): TSharedState | null {
		this.#ensureNotDisposed();
		return this.#session.getSharedStateJson<TSharedState>() ?? null;
	}

	/** Get the result for a partition. V1: transformed state (after `transformBy`/`filterBy`), or state if no transform. V2: post-handler state (transforms aren't invoked). */
	getResult(
		/** Partition key. */
		partition?: string,
	): TResult | null {
		this.#ensureNotDisposed();
		return this.#session.getResultJson<TResult>(partition) ?? null;
	}

	/** Release native resources. Safe to call multiple times. */
	dispose(): void {
		if (this.#disposed) return;
		this.#disposed = true;
		registry.unregister(this);
		this.#session.dispose();
	}

	[Symbol.dispose](): void {
		this.dispose();
	}

	#ensureNotDisposed(): void {
		if (this.#disposed) {
			throw new Error("ProjectionTest has been disposed");
		}
	}
}

export function toSessionOptions(options: ProjectionOptions): SessionOptions {
	const out: SessionOptions = { engineVersion: options.engineVersion };
	if (options.quirksVersion !== undefined) {
		out.quirksVersion = options.quirksVersion;
	}
	const executionTimeoutMs =
		options.config?.executionTimeoutMs ??
		options.databaseConfig?.executionTimeoutMs;
	if (executionTimeoutMs !== undefined) {
		out.executionTimeoutMs = executionTimeoutMs;
	}
	const compilationTimeoutMs = options.databaseConfig?.compilationTimeoutMs;
	if (compilationTimeoutMs !== undefined) {
		out.compilationTimeoutMs = compilationTimeoutMs;
	}
	return out;
}

function mapStepResult<TState, TResult, TSharedState>(
	feed: FeedResult,
	input: EventInput,
): StepResult<TState, TResult, TSharedState> {
	if (feed.status === "skipped") {
		return {
			status: "skipped",
			reason: feed.reason as SkipReason,
			event: input,
		};
	}

	return {
		status: "processed",
		state: feed.state as TState,
		result: feed.result as TResult,
		...(feed.sharedState !== undefined && {
			sharedState: feed.sharedState as TSharedState,
		}),
		...(feed.partition !== undefined && { partition: feed.partition }),
		event: input,
		emitted: (feed.emitted ?? []).map(mapEmittedEvent),
		logs: feed.logs ?? [],
	} as ProcessedStepResult<TState, TResult, TSharedState>;
}

function mapEmittedEvent(event: EmittedEvent): TestEmittedEvent {
	let data: unknown = null;
	if (event.data) {
		try {
			data = JSON.parse(event.data);
		} catch {
			data = event.data;
		}
	}

	return {
		streamId: event.streamId,
		eventType: event.eventType,
		data,
		metadata: event.metadata ?? null,
		isLink: event.isLink,
	};
}
