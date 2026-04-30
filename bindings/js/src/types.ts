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
