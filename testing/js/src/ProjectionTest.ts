import {
	ProjectionSession,
	type EmittedEvent,
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

/** Result of feeding a single event to a projection. */
export interface StepResult<
	TState = unknown,
	TResult = unknown,
	TSharedState = unknown,
> {
	/** Projection state for the affected partition, or null if no state change. */
	state: TState | null;
	/** Transformed result (after `transformBy`/`filterBy`), or state if no transform is defined. */
	result: TResult | null;
	/** Shared state for biState projections, or null if not biState. */
	sharedState: TSharedState | null;
	/** Partition key that was updated, or null if unpartitioned or no state change. */
	partition: string | null;
	/** The input event that was fed. */
	event: EventInput;
	/** Events emitted during processing of this event. */
	emitted: TestEmittedEvent[];
	/** Log messages from `log()` calls during processing of this event. */
	logs: string[];
}

/** Per-projection configuration. */
export interface ProjectionConfig {
	/** When true, validates event content types. V2 only. */
	enableContentTypeValidation?: boolean;
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
	/** Projection engine version. Default: "v2". */
	version?: "v1" | "v2";
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
	TResult = unknown,
	TSharedState = unknown,
> {
	private session: ProjectionSession;
	private disposed = false;
	private pendingEmitted: TestEmittedEvent[] = [];
	private pendingLogs: string[] = [];
	private stateChanged = false;
	private lastRawPartition = "";

	constructor(source: string, options?: ProjectionOptions) {
		this.session = new ProjectionSession(source, toSessionOptions(options));
		registry.register(this, this.session, this);

		this.session.onEmit((event: EmittedEvent) => {
			this.pendingEmitted.push(mapEmittedEvent(event));
		});

		this.session.onLog((message: string) => {
			this.pendingLogs.push(message);
		});

		this.session.onStateChanged((partition: string) => {
			this.stateChanged = true;
			this.lastRawPartition = partition;
		});
	}

	/**
	 * Feed a single event to the projection. Returns the state and any emitted events/logs.
	 * @throws {ProjectionError} If the projection handler throws, with `input` and `normalized` fields attached.
	 */
	feed(
		/** A TestEvent, RecordedEvent, or ResolvedEvent. */
		input: EventInput,
	): StepResult<TState, TResult, TSharedState> {
		this.ensureNotDisposed();

		const parsed = parseEventInput(input);
		const normalized = normalizeEvent(parsed);

		this.pendingEmitted = [];
		this.pendingLogs = [];
		this.stateChanged = false;
		this.lastRawPartition = "";

		this.session.feed(normalized);

		const partition = this.stateChanged ? this.lastRawPartition || null : null;

		return {
			state: this.stateChanged
				? (this.session.getStateJson<TState>(this.lastRawPartition) ?? null)
				: null,
			result: this.stateChanged
				? (this.session.getResultJson<TResult>(this.lastRawPartition) ?? null)
				: null,
			sharedState: this.session.getSharedStateJson<TSharedState>() ?? null,
			partition,
			event: input,
			emitted: this.pendingEmitted,
			logs: this.pendingLogs,
		};
	}

	/** Get current state for a partition. Pass no argument for unpartitioned projections. */
	getState(
		/** Partition key. */
		partition?: string,
	): TState | null {
		this.ensureNotDisposed();
		return this.session.getStateJson<TState>(partition) ?? null;
	}

	/** Get shared state for biState projections. */
	getSharedState(): TSharedState | null {
		this.ensureNotDisposed();
		return this.session.getSharedStateJson<TSharedState>() ?? null;
	}

	/** Get the transformed result (after `transformBy`/`filterBy`) for a partition, or state if no transform is defined. */
	getResult(
		/** Partition key. */
		partition?: string,
	): TResult | null {
		this.ensureNotDisposed();
		return this.session.getResultJson<TResult>(partition) ?? null;
	}

	/** Release native resources. Safe to call multiple times. */
	dispose(): void {
		if (this.disposed) return;
		this.disposed = true;
		registry.unregister(this);
		this.session.dispose();
	}

	[Symbol.dispose](): void {
		this.dispose();
	}

	private ensureNotDisposed(): void {
		if (this.disposed) {
			throw new Error("ProjectionTest has been disposed");
		}
	}
}

export function toSessionOptions(
	options?: ProjectionOptions,
): SessionOptions | undefined {
	if (!options) return undefined;
	return {
		version: options.version,
		enableContentTypeValidation: options.config?.enableContentTypeValidation,
		executionTimeoutMs:
			options.config?.executionTimeoutMs ??
			options.databaseConfig?.executionTimeoutMs,
		compilationTimeoutMs: options.databaseConfig?.compilationTimeoutMs,
	};
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
