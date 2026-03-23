export class GafferError extends Error {
	readonly code: string;
	readonly description: string;
	override readonly cause: unknown;

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

export class InvalidProjectionError extends GafferError {
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
		this.location = location;
	}
}

export class CompilationTimeoutError extends GafferError {
	readonly elapsed: number;
	readonly allowed: number;

	constructor(
		description: string,
		elapsed: number,
		allowed: number,
		cause?: unknown,
	) {
		super("compilation-timeout", description, cause);
		this.elapsed = elapsed;
		this.allowed = allowed;
	}
}

export class InvalidArgumentError extends GafferError {
	readonly field: string;

	constructor(description: string, field: string, cause?: unknown) {
		super("invalid-argument", description, cause);
		this.field = field;
	}
}

export interface EventContext {
	eventType: string;
	streamId: string;
	sequenceNumber: number;
	partition?: string;
}

export class ProjectionHandlerError extends GafferError {
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
		this.jsStack = jsStack;
		this.location = location;
	}
}

export class ExecutionTimeoutError extends GafferError {
	readonly elapsed: number;
	readonly allowed: number;
	readonly event: EventContext;

	constructor(
		description: string,
		elapsed: number,
		allowed: number,
		event: EventContext,
		cause?: unknown,
	) {
		super("execution-timeout", description, cause);
		this.elapsed = elapsed;
		this.allowed = allowed;
		this.event = event;
	}
}

export class MalformedEventError extends GafferError {
	readonly event: EventContext;

	constructor(description: string, event: EventContext, cause?: unknown) {
		super("malformed-event", description, cause);
		this.event = event;
	}
}

export class StateSerializationError extends GafferError {
	readonly event: EventContext;

	constructor(description: string, event: EventContext, cause?: unknown) {
		super("state-serialization-error", description, cause);
		this.event = event;
	}
}

export class ProjectionTransformError extends GafferError {
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
		this.jsStack = jsStack;
		this.location = location;
	}
}

interface ErrorJson {
	code: string;
	description: string;
	message?: string;
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

export function parseErrorJson(json: string, source: string): GafferError {
	const err: ErrorJson = JSON.parse(json);

	switch (err.code) {
		case "invalid-projection":
			return new InvalidProjectionError(
				err.description,
				source,
				err.line != null ? { line: err.line, column: err.column! } : undefined,
				undefined,
				err.message,
			);

		case "compilation-timeout":
			return new CompilationTimeoutError(
				err.description,
				err.elapsed!,
				err.allowed!,
			);

		case "invalid-argument":
			return new InvalidArgumentError(err.description, err.field!);

		case "handler-error":
			return new ProjectionHandlerError(
				err.description,
				{
					eventType: err.eventType!,
					streamId: err.streamId!,
					sequenceNumber: err.sequenceNumber!,
					partition: err.partition,
				},
				source,
				err.jsStack,
				err.line != null ? { line: err.line, column: err.column! } : undefined,
				undefined,
				err.message,
			);

		case "execution-timeout":
			return new ExecutionTimeoutError(
				err.description,
				err.elapsed!,
				err.allowed!,
				{
					eventType: err.eventType!,
					streamId: err.streamId!,
					sequenceNumber: err.sequenceNumber!,
					partition: err.partition,
				},
			);

		case "malformed-event":
			return new MalformedEventError(err.description, {
				eventType: err.eventType!,
				streamId: err.streamId!,
				sequenceNumber: err.sequenceNumber!,
				partition: err.partition,
			});

		case "state-serialization-error":
			return new StateSerializationError(err.description, {
				eventType: err.eventType!,
				streamId: err.streamId!,
				sequenceNumber: err.sequenceNumber!,
				partition: err.partition,
			});

		case "projection-transform-error":
			return new ProjectionTransformError(
				err.description,
				source,
				err.jsStack,
				err.line != null ? { line: err.line, column: err.column! } : undefined,
				undefined,
				err.message,
			);

		default:
			return new GafferError(err.code, err.description);
	}
}
