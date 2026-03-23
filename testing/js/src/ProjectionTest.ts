import { ProjectionSession, type EmittedEvent } from "@kurrent/gaffer-runtime";
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
	metadata: Record<string, unknown> | null;
	/** True for `linkTo()` events (`$>` or `$@` event types). */
	isLink: boolean;
}

/** Result of feeding a single event to a projection. */
export interface StepResult<TState = unknown> {
	/** Projection state after processing this event. */
	state: TState | null;
	/** The input event that was fed. */
	event: EventInput;
	/** Events emitted during processing of this event. */
	emitted: TestEmittedEvent[];
	/** Log messages from `log()` calls during processing of this event. */
	logs: string[];
}

/** Options for configuring a projection session. */
export interface ProjectionOptions {
	/** Projection engine version. "v1" drops non-JSON events. Default: "v2". */
	version?: "v1" | "v2";
	/** Maximum time for JS compilation in ms. Default: 5000. */
	compilationTimeoutMs?: number;
	/** Maximum time for JS handler execution per event in ms. Default: 5000. */
	executionTimeoutMs?: number;
	/** Threshold in ms for onSlowHandler warnings. Default: 250. */
	handlerTimeoutMs?: number;
	/** When true, validates event content types. V2 only. Default: false. */
	enableContentTypeValidation?: boolean;
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
export class ProjectionTest<TState = unknown> {
	private session: ProjectionSession;
	private disposed = false;
	private pendingEmitted: TestEmittedEvent[] = [];
	private pendingLogs: string[] = [];

	constructor(source: string, options?: ProjectionOptions) {
		this.session = new ProjectionSession(source, options);
		registry.register(this, this.session, this);

		this.session.onEmit((event: EmittedEvent) => {
			this.pendingEmitted.push(mapEmittedEvent(event));
		});

		this.session.onLog((message: string) => {
			this.pendingLogs.push(message);
		});
	}

	/**
	 * Feed a single event to the projection. Returns the state and any emitted events/logs.
	 * @throws {ProjectionError} If the projection handler throws, with `input` and `normalized` fields attached.
	 */
	feed(
		/** A TestEvent, RecordedEvent, or ResolvedEvent. */
		input: EventInput,
	): StepResult<TState> {
		this.ensureNotDisposed();

		const parsed = parseEventInput(input);
		const normalized = normalizeEvent(parsed);

		this.pendingEmitted = [];
		this.pendingLogs = [];

		this.session.feed(normalized);

		const state = this.session.getStateJson<TState>() ?? null;

		return {
			state,
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
	getSharedState(): unknown | null {
		this.ensureNotDisposed();
		return this.session.getSharedStateJson() ?? null;
	}

	/** Get the transformed result (after `transformBy`/`filterBy`) for a partition. */
	getResult(
		/** Partition key. */
		partition?: string,
	): TState | null {
		this.ensureNotDisposed();
		return this.session.getResultJson<TState>(partition) ?? null;
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
		metadata: event.metadata
			? (event.metadata as Record<string, unknown>)
			: null,
		isLink: event.isLink,
	};
}
