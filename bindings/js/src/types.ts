/** An event to feed into a projection session. */
export interface ProjectionEvent {
	eventType: string;
	streamId: string;
	sequenceNumber: number;
	isJson: boolean;
	eventId: string;
	created: string;
	data?: string;
	metadata?: string;
	linkMetadata?: string;
}

/** An event emitted by a projection via emit() or linkTo(). */
export interface EmittedEvent {
	streamId: string;
	eventType: string;
	data: string | null;
	isJson: boolean;
	isLink: boolean;
	metadata: Record<string, string | null> | null;
}

/** Projection source definition - what the projection reads. */
export interface ProjectionInfo {
	allStreams: boolean;
	allEvents: boolean;
	categories: string[] | null;
	streams: string[] | null;
	events: string[] | null;
	byStreams: boolean;
	byCustomPartitions: boolean;
	biState: boolean;
	definesHandlers: boolean;
	definesStateTransform: boolean;
	producesResults: boolean;
	handlesDeletedNotifications: boolean;
	includeLinks: boolean;
	resultStreamName: string | null;
	partitionResultStreamNamePattern: string | null;
	reorderEvents: boolean;
	processingLag: number | null;
	diagnostics: Diagnostic[] | null;
}

/**
 * A compile-time diagnostic emitted by the runtime.
 *
 * `code` is namespaced as `<category>.<name>` (e.g. `deprecated.linkStreamTo`).
 * `range` is null when the diagnostic has no associated source location.
 */
export interface Diagnostic {
	code: string;
	message: string;
	severity: DiagnosticSeverity;
	range: SourceRange | null;
}

/**
 * Severity of a {@link Diagnostic}. Values match the LSP `DiagnosticSeverity`
 * enum so editor adapters can pass them through unchanged.
 */
export const DiagnosticSeverity = {
	Error: 1,
	Warning: 2,
	Information: 3,
	Hint: 4,
} as const;
export type DiagnosticSeverity =
	(typeof DiagnosticSeverity)[keyof typeof DiagnosticSeverity];

/** Inclusive start, exclusive end - matches LSP and most editor APIs. */
export interface SourceRange {
	start: SourcePosition;
	end: SourcePosition;
}

/**
 * 1-based line and column in projection source. LSP clients subtract 1 from
 * each at the boundary.
 */
export interface SourcePosition {
	line: number;
	column: number;
}

/** Result of feeding a single event to a projection session. */
export interface FeedResult {
	status: "processed" | "skipped";
	reason?: string;
	partition?: string;
	state?: unknown;
	result?: unknown;
	sharedState?: unknown;
	emitted?: EmittedEvent[];
	logs?: string[];
}

/** Options for creating a projection session. */
export interface SessionOptions {
	/** Projection engine version. 1 drops non-JSON events. Required. */
	engineVersion: 1 | 2;
	compilationTimeoutMs?: number;
	executionTimeoutMs?: number;
	debug?: boolean;
}
