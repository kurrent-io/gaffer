import type { QuerySources } from "@kurrent/gaffer-runtime";

/** Cleaned-up projection source definition, mapped from the runtime's raw QuerySources. */
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

/** Map the runtime's raw QuerySources to a cleaner ProjectionInfo shape. */
export function mapQuerySources(raw: QuerySources): ProjectionInfo {
	let source: ProjectionInfo["source"];
	if (raw.AllStreams) {
		source = { type: "all" };
	} else if (raw.Categories && raw.Categories.length > 0) {
		source = { type: "categories", categories: raw.Categories };
	} else if (raw.Streams && raw.Streams.length > 0) {
		source = { type: "streams", streams: raw.Streams };
	} else {
		source = { type: "all" };
	}

	let partitioning: ProjectionInfo["partitioning"];
	if (raw.ByStreams) {
		partitioning = { type: "byStream" };
	} else if (raw.ByCustomPartitions) {
		partitioning = { type: "byCustomKey" };
	} else {
		partitioning = { type: "none" };
	}

	return {
		source,
		partitioning,
		events: raw.AllEvents ? "all" : (raw.Events ?? []),
		biState: raw.IsBiState,
		settings: {
			includeLinks: raw.IncludeLinks,
			reorderEvents: raw.ReorderEvents,
			processingLag: raw.ProcessingLag,
			resultStreamName: raw.ResultStreamName,
			partitionResultStreamNamePattern: raw.PartitionResultStreamNamePattern,
			handlesDeletedNotifications: raw.HandlesDeletedNotifications,
		},
	};
}
