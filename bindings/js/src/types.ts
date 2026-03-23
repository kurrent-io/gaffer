/** An event to feed into a projection session. */
export interface ProjectionEvent {
	eventType: string;
	streamId: string;
	sequenceNumber: number;
	isJson: boolean;
	eventId: string;
	timestamp: string;
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
export interface QuerySources {
	AllStreams: boolean;
	AllEvents: boolean;
	Categories: string[] | null;
	Streams: string[] | null;
	Events: string[] | null;
	ByStreams: boolean;
	ByCustomPartitions: boolean;
	IsBiState: boolean;
	DefinesFold: boolean;
	DefinesStateTransform: boolean;
	ProducesResults: boolean;
	HandlesDeletedNotifications: boolean;
	IncludeLinks: boolean;
	ResultStreamName: string | null;
	PartitionResultStreamNamePattern: string | null;
	ReorderEvents: boolean;
	ProcessingLag: number | null;
}

/** Options for creating a projection session. */
export interface SessionOptions {
	/** Projection engine version. "v1" drops non-JSON events. Default: "v2". */
	version?: "v1" | "v2";
	compilationTimeoutMs?: number;
	executionTimeoutMs?: number;
	enableContentTypeValidation?: boolean;
	debug?: boolean;
}
