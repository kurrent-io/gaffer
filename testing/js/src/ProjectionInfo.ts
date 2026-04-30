import type { ProjectionInfo as RawProjectionInfo } from "@kurrent/gaffer-runtime";

/** Cleaned-up projection source definition, mapped from the runtime's raw ProjectionInfo. */
export interface ProjectionInfo {
	/** What the projection reads from: all streams, specific streams, or categories. */
	source:
		| { type: "all" }
		| { type: "streams"; streams: string[] }
		| { type: "categories"; categories: string[] };

	/** How events are partitioned: none, by stream (foreachStream), or by custom key (partitionBy). */
	partitioning:
		| { type: "none" }
		| { type: "byStream" }
		| { type: "byCustomKey" };

	/** Event types the projection handles, or "all" if it handles any event type. */
	events: string[] | "all";
	/** Whether this is a biState projection with separate shared state. */
	biState: boolean;

	/** Projection configuration settings. */
	settings: {
		/** Whether link events are included in processing. */
		includeLinks: boolean;
		/** Whether events are reordered by timestamp before processing. */
		reorderEvents: boolean;
		/** Processing lag in milliseconds, or null if not set. */
		processingLag: number | null;
		/** Output stream name for `outputState()`, or null. */
		resultStreamName: string | null;
		/** Pattern for per-partition result streams, or null. */
		partitionResultStreamNamePattern: string | null;
		/** Whether the projection defines a `$deleted` handler. */
		handlesDeletedNotifications: boolean;
	};
}

/** Map the runtime's raw ProjectionInfo to a cleaner shape with discriminated unions. */
export function mapProjectionInfo(raw: RawProjectionInfo): ProjectionInfo {
	let source: ProjectionInfo["source"];
	if (raw.allStreams) {
		source = { type: "all" };
	} else if (raw.categories && raw.categories.length > 0) {
		source = { type: "categories", categories: raw.categories };
	} else if (raw.streams && raw.streams.length > 0) {
		source = { type: "streams", streams: raw.streams };
	} else {
		source = { type: "all" };
	}

	let partitioning: ProjectionInfo["partitioning"];
	if (raw.byStreams) {
		partitioning = { type: "byStream" };
	} else if (raw.byCustomPartitions) {
		partitioning = { type: "byCustomKey" };
	} else {
		partitioning = { type: "none" };
	}

	return {
		source,
		partitioning,
		events: raw.allEvents ? "all" : (raw.events ?? []),
		biState: raw.biState,
		settings: {
			includeLinks: raw.includeLinks,
			reorderEvents: raw.reorderEvents,
			processingLag: raw.processingLag,
			resultStreamName: raw.resultStreamName,
			partitionResultStreamNamePattern: raw.partitionResultStreamNamePattern,
			handlesDeletedNotifications: raw.handlesDeletedNotifications,
		},
	};
}
