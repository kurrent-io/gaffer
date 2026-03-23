import {
	streamNameFilter,
	eventTypeFilter,
	type Filter,
} from "@kurrent/kurrentdb-client";
import type { ProjectionInfo } from "./ProjectionInfo.js";

/**
 * Build a subscription filter from a projection's source definition.
 *
 * For fromAll() with specific events, filters by event type prefix.
 * For fromStreams(), filters by exact stream name regex.
 * For fromCategory(), filters by category prefix (category + "-").
 * Returns undefined for fromAll() with all events (no filter needed).
 */
export function buildSubscriptionFilter(
	info: ProjectionInfo,
): Filter | undefined {
	switch (info.source.type) {
		case "all":
			return info.events !== "all"
				? eventTypeFilter({ prefixes: info.events })
				: undefined;
		case "streams":
			return streamNameFilter({
				regex: `^(${info.source.streams.map(escapeRegex).join("|")})$`,
			});
		case "categories":
			return streamNameFilter({
				prefixes: info.source.categories.map((c) => c + "-"),
			});
	}
}

/** Escape special regex characters in a string. */
export const escapeRegex = (s: string) =>
	s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
