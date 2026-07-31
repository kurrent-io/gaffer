import type { Diagnostic } from "./types.js";

/**
 * Base class for every error the runtime throws. `code` is the stable
 * machine-readable discriminator (each subclass fixes its own);
 * `message` carries the runtime's formatted report, with a source
 * snippet and caret where the error has a location.
 */
export class ProjectionError extends Error {
	readonly code: string;
	readonly description: string;
	override readonly cause: unknown;
	/**
	 * Diagnostic catalogue code (e.g. `quirk.event.bodyCast`) when this error
	 * was thrown by an upstream-quirk-compat code path. This is the code of the
	 * quirk that threw; it is also present in {@link diagnostics}, which is the
	 * complete set of quirks that fired on this event. Set by `parseErrorJson`
	 * from the runtime payload; not part of any subclass constructor.
	 */
	compatCode?: string;
	/** Catalogue description of {@link compatCode}, when set. */
	compatDescription?: string;
	/** KurrentDB version that fixes {@link compatCode} upstream, when known. */
	compatFixedIn?: string;
	/**
	 * Quirks that fired while processing the event that threw, including the
	 * throwing quirk ({@link compatCode}). Empty/undefined unless a quirk was
	 * exercised. Gives a throwing quirk the same diagnostics channel as a
	 * non-throwing one (which surfaces on `FeedResult.diagnostics`). Set by
	 * `parseErrorJson`.
	 */
	diagnostics?: Diagnostic[];

	constructor(
		code: string,
		description: string,
		cause?: unknown,
		message?: string,
	) {
		super(message ?? description);
		this.name = this.constructor.name;
		this.code = code;
		this.description = description;
		this.cause = cause;
	}
}

/**
 * The projection source failed to compile or validate
 * (code `invalid-projection`).
 */
export class InvalidProjectionError extends ProjectionError {
	readonly location?: { line: number; column: number };
	readonly source: string;

	constructor(
		description: string,
		source: string,
		location?: { line: number; column: number },
		cause?: unknown,
		message?: string,
	) {
		super("invalid-projection", description, cause, message);
		this.source = source;
		if (location !== undefined) this.location = location;
	}
}

/**
 * Compiling the projection exceeded the allowed time
 * (code `compilation-timeout`).
 */
export class CompilationTimeoutError extends ProjectionError {
	readonly elapsed: number;
	readonly allowed: number;

	constructor(
		description: string,
		elapsed: number,
		allowed: number,
		cause?: unknown,
		message?: string,
	) {
		super("compilation-timeout", description, cause, message);
		this.elapsed = elapsed;
		this.allowed = allowed;
	}
}

/**
 * A call was made with an invalid argument (code `invalid-argument`);
 * `field` names the offending one.
 */
export class InvalidArgumentError extends ProjectionError {
	readonly field: string;

	constructor(
		description: string,
		field: string,
		cause?: unknown,
		message?: string,
	) {
		super("invalid-argument", description, cause, message);
		this.field = field;
	}
}

/** The event being processed when an error was thrown. */
export interface EventContext {
	eventType: string;
	streamId: string;
	sequenceNumber: number;
	partition?: string;
}

/**
 * A handler threw while processing an event (code `handler-error`),
 * with the JS stack, source location, and the event's context.
 */
export class ProjectionHandlerError extends ProjectionError {
	readonly jsStack?: string;
	readonly location?: { line: number; column: number };
	readonly event: EventContext;
	readonly source: string;

	constructor(
		description: string,
		event: EventContext,
		source: string,
		jsStack?: string,
		location?: { line: number; column: number },
		cause?: unknown,
		message?: string,
	) {
		super("handler-error", description, cause, message);
		this.event = event;
		this.source = source;
		if (jsStack !== undefined) this.jsStack = jsStack;
		if (location !== undefined) this.location = location;
	}
}

/**
 * Processing an event exceeded the allowed time
 * (code `execution-timeout`).
 */
export class ExecutionTimeoutError extends ProjectionError {
	readonly elapsed: number;
	readonly allowed: number;
	readonly event: EventContext;

	constructor(
		description: string,
		elapsed: number,
		allowed: number,
		event: EventContext,
		cause?: unknown,
		message?: string,
	) {
		super("execution-timeout", description, cause, message);
		this.elapsed = elapsed;
		this.allowed = allowed;
		this.event = event;
	}
}

/**
 * The engine couldn't parse or feed an event (code `malformed-event`).
 */
export class MalformedEventError extends ProjectionError {
	readonly event: EventContext;

	constructor(
		description: string,
		event: EventContext,
		cause?: unknown,
		message?: string,
	) {
		super("malformed-event", description, cause, message);
		this.event = event;
	}
}

/**
 * State couldn't be serialized after a handler ran
 * (code `state-serialization-error`).
 */
export class StateSerializationError extends ProjectionError {
	readonly event: EventContext;

	constructor(
		description: string,
		event: EventContext,
		cause?: unknown,
		message?: string,
	) {
		super("state-serialization-error", description, cause, message);
		this.event = event;
	}
}

/**
 * A projection transform function threw
 * (code `projection-transform-error`).
 */
export class ProjectionTransformError extends ProjectionError {
	readonly jsStack?: string;
	readonly location?: { line: number; column: number };
	readonly source: string;

	constructor(
		description: string,
		source: string,
		jsStack?: string,
		location?: { line: number; column: number },
		cause?: unknown,
		message?: string,
	) {
		super("projection-transform-error", description, cause, message);
		this.source = source;
		if (jsStack !== undefined) this.jsStack = jsStack;
		if (location !== undefined) this.location = location;
	}
}

interface ErrorJson {
	code: string;
	description: string;
	message?: string;
	compatCode?: string;
	compatDescription?: string;
	compatFixedIn?: string;
	diagnostics?: Diagnostic[];
	line?: number;
	column?: number;
	elapsed?: number;
	allowed?: number;
	field?: string;
	jsStack?: string;
	eventType?: string;
	streamId?: string;
	sequenceNumber?: number;
	partition?: string;
}

function required<T>(value: T | undefined, code: string, field: string): T {
	if (value === undefined) {
		throw new Error(`malformed ${code} error from runtime: missing ${field}`);
	}
	return value;
}

function eventContextOf(err: ErrorJson): EventContext {
	const ctx: EventContext = {
		eventType: required(err.eventType, err.code, "eventType"),
		streamId: required(err.streamId, err.code, "streamId"),
		sequenceNumber: required(err.sequenceNumber, err.code, "sequenceNumber"),
	};
	if (err.partition !== undefined) ctx.partition = err.partition;
	return ctx;
}

function locationOf(
	err: ErrorJson,
): { line: number; column: number } | undefined {
	if (err.line == null) return undefined;
	return { line: err.line, column: required(err.column, err.code, "column") };
}

export function parseErrorJson(json: string, source: string): ProjectionError {
	const err: ErrorJson = JSON.parse(json);
	const result = constructError(err, source);
	if (err.compatCode !== undefined) result.compatCode = err.compatCode;
	if (err.compatDescription !== undefined)
		result.compatDescription = err.compatDescription;
	if (err.compatFixedIn !== undefined) result.compatFixedIn = err.compatFixedIn;
	if (err.diagnostics !== undefined) result.diagnostics = err.diagnostics;
	return result;
}

function constructError(err: ErrorJson, source: string): ProjectionError {
	switch (err.code) {
		case "invalid-projection":
			return new InvalidProjectionError(
				err.description,
				source,
				locationOf(err),
				undefined,
				err.message,
			);

		case "compilation-timeout":
			return new CompilationTimeoutError(
				err.description,
				required(err.elapsed, err.code, "elapsed"),
				required(err.allowed, err.code, "allowed"),
				undefined,
				err.message,
			);

		case "invalid-argument":
			return new InvalidArgumentError(
				err.description,
				required(err.field, err.code, "field"),
				undefined,
				err.message,
			);

		case "handler-error":
			return new ProjectionHandlerError(
				err.description,
				eventContextOf(err),
				source,
				err.jsStack,
				locationOf(err),
				undefined,
				err.message,
			);

		case "execution-timeout":
			return new ExecutionTimeoutError(
				err.description,
				required(err.elapsed, err.code, "elapsed"),
				required(err.allowed, err.code, "allowed"),
				eventContextOf(err),
				undefined,
				err.message,
			);

		case "malformed-event":
			return new MalformedEventError(
				err.description,
				eventContextOf(err),
				undefined,
				err.message,
			);

		case "state-serialization-error":
			return new StateSerializationError(
				err.description,
				eventContextOf(err),
				undefined,
				err.message,
			);

		case "projection-transform-error":
			return new ProjectionTransformError(
				err.description,
				source,
				err.jsStack,
				locationOf(err),
				undefined,
				err.message,
			);

		default:
			return new ProjectionError(err.code, err.description);
	}
}
