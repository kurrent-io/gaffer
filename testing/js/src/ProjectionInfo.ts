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
		/** Whether the projection writes events (`emit`, `linkTo`, `linkStreamTo`, or `copyTo`). */
		emitsEvents: boolean;
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

/**
 * Whether an event on `streamId` would be delivered to a projection with this
 * source definition. Real KurrentDB only feeds a handler events from the
 * streams its source declares; the testing library mirrors that at `feed()`
 * time. Stream/prefix semantics match `buildSubscriptionFilter` (and the
 * subscription spec) so the manual and live-client paths agree.
 */
export function streamMatchesSource(
	info: ProjectionInfo,
	streamId: string,
): boolean {
	switch (info.source.type) {
		case "all":
			return true;
		case "streams": {
			const streams = info.source.streams;
			// fromCategory multi-arg lands all-$ce-<cat> entries here; match by
			// category prefix. Any other shape (including a mix) is an exact
			// stream-name match, mirroring buildSubscriptionFilter's branching so
			// feed() and the live run(client) path never disagree.
			if (streams.every((s) => s.startsWith("$ce-"))) {
				return streams.some((s) =>
					streamId.startsWith(`${s.slice("$ce-".length)}-`),
				);
			}
			return streams.includes(streamId);
		}
		case "categories":
			return info.source.categories.some((c) => streamId.startsWith(`${c}-`));
	}
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
			emitsEvents: raw.emitsEvents,
			includeLinks: raw.includeLinks,
			reorderEvents: raw.reorderEvents,
			processingLag: raw.processingLag,
			resultStreamName: raw.resultStreamName,
			partitionResultStreamNamePattern: raw.partitionResultStreamNamePattern,
			handlesDeletedNotifications: raw.handlesDeletedNotifications,
		},
	};
}
