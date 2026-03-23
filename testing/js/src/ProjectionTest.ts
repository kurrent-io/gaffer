import { ProjectionSession, type EmittedEvent } from "@kurrent/gaffer-runtime";
import {
	parseEventInput,
	normalizeEvent,
	type EventInput,
	type NormalizedEvent,
} from "./schemas.js";

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

	constructor(source: string) {
		this.session = new ProjectionSession(source);
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
	 * @throws {ProjectionError} If the projection handler throws.
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

		try {
			this.session.feed(normalized);
		} catch (err) {
			throw new ProjectionError(normalized, input, err);
		}

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
		isLink: event.eventType === "$>" || event.eventType === "$@",
	};
}

/** Thrown when a projection's JavaScript source fails to compile. */
export class InvalidProjectionError extends Error {
	/** The original error from the runtime. */
	override cause: unknown;

	constructor(cause: unknown) {
		const message = cause instanceof Error ? cause.message : String(cause);
		super(`Invalid projection: ${message}`);
		this.name = "InvalidProjectionError";
		this.cause = cause;
	}
}

/**
 * Thrown when a projection handler throws during event processing.
 * Contains the original event, normalized fields, and the underlying error as `cause`.
 */
export class ProjectionError extends Error {
	/** The original error from the runtime. */
	override cause: unknown;
	/** The original event input that caused the error. */
	event: EventInput;
	/** Normalized event fields (eventType, streamId, data, sequenceNumber, etc). */
	normalized: NormalizedEvent;

	constructor(normalized: NormalizedEvent, event: EventInput, cause: unknown) {
		const causeMessage = cause instanceof Error ? cause.message : String(cause);
		const ref =
			normalized.sequenceNumber !== undefined
				? `${normalized.sequenceNumber}@${normalized.streamId}`
				: normalized.streamId;

		const lines = [
			"Projection error",
			`  Event: ${ref}`,
			`  Type:  ${normalized.eventType}`,
		];
		if (normalized.data) {
			lines.push(`  Data:  ${normalized.data}`);
		}
		lines.push(`  Error: ${causeMessage}`);

		super(lines.join("\n"));
		this.name = "ProjectionError";
		this.cause = cause;
		this.event = event;
		this.normalized = normalized;
	}
}
