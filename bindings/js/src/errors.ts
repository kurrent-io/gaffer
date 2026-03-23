function formatSnippet(
	source: string,
	description: string,
	location: { line: number; column: number },
): string {
	const lines = source.split("\n");
	const { line, column } = location;
	const errorLine = lines[line - 1] ?? "";
	const startContext = Math.max(0, line - 3);
	const contextLines = lines.slice(startContext, line - 1);
	const gutter = String(line).length + 1;

	const allLines = [...contextLines, errorLine];
	const minIndent = allLines.reduce((min, l) => {
		if (l.trim().length === 0) return min;
		const indent = l.match(/^[\t ]*/)?.[0].length ?? 0;
		return Math.min(min, indent);
	}, Infinity);
	const strip = minIndent === Infinity ? 0 : minIndent;

	const pad = " ".repeat(gutter);
	let result = ` ${pad} ┌─ ${line}:${column - strip + 1}\n`;
	result += ` ${pad} │\n`;

	for (let i = 0; i < contextLines.length; i++) {
		const num = startContext + i + 1;
		result += ` ${String(num).padStart(gutter)} │ ${contextLines[i].slice(strip)}\n`;
	}

	const strippedError = errorLine.slice(strip);
	const adjustedColumn = column - strip;
	result += ` ${String(line).padStart(gutter)} │ ${strippedError}\n`;
	const caretPad = strippedError
		.slice(0, adjustedColumn)
		.replace(/[^\t]/g, " ");
	result += ` ${pad} │ ${caretPad}^ ${description}\n`;
	result += ` ${pad} │\n`;

	return result;
}

function formatEventContext(event: EventContext): string {
	let result = `Event: ${event.sequenceNumber}@${event.streamId}\n`;
	result += `Type:  ${event.eventType}\n`;
	if (event.partition) {
		result += `Partition: ${event.partition}\n`;
	}
	return result;
}

function formatInvalidProjectionMessage(
	description: string,
	source: string,
	location?: { line: number; column: number },
): string {
	if (!location) {
		return `Invalid projection definition\n\nerror: ${description}\n`;
	}
	return (
		`Failed to compile projection\n\nerror: ${description}\n` +
		formatSnippet(source, description, location)
	);
}

function formatJsError(
	description: string,
	source: string,
	jsStack?: string,
	location?: { line: number; column: number },
): string {
	let result = `error: ${description}\n`;
	if (location) {
		result += formatSnippet(source, description, location);
	}
	if (jsStack) {
		for (const line of jsStack.split("\n")) {
			result += `  ${line.trim()}\n`;
		}
	}
	return result;
}

function formatHandlerMessage(
	description: string,
	source: string,
	event: EventContext,
	jsStack?: string,
	location?: { line: number; column: number },
): string {
	let result = `Error processing event\n\n`;
	result += formatJsError(description, source, jsStack, location);
	result += `\n`;
	result += formatEventContext(event);
	return result;
}

function formatTransformMessage(
	description: string,
	source: string,
	jsStack?: string,
	location?: { line: number; column: number },
): string {
	let result = `Transform error\n\n`;
	result += formatJsError(description, source, jsStack, location);
	return result;
}

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
	) {
		super(
			"invalid-projection",
			description,
			cause,
			formatInvalidProjectionMessage(description, source, location),
		);
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
	) {
		super(
			"handler-error",
			description,
			cause,
			formatHandlerMessage(description, source, event, jsStack, location),
		);
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
	) {
		super(
			"projection-transform-error",
			description,
			cause,
			formatTransformMessage(description, source, jsStack, location),
		);
		this.source = source;
		this.jsStack = jsStack;
		this.location = location;
	}
}

interface ErrorJson {
	code: string;
	description: string;
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
			);

		default:
			return new GafferError(err.code, err.description);
	}
}
