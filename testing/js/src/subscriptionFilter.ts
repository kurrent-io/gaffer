import {
	streamNameFilter,
	eventTypeFilter,
	type Filter,
} from "@kurrent/kurrentdb-client";
import type { ProjectionInfo } from "./ProjectionInfo.js";

// Read-window defaults for filtered subscriptions. The TS client's
// own defaults (maxSearchWindow=32, checkpointInterval=1) make
// CaughtUp effectively never fire on a busy store - see specs/
// subscription.md "Subscription read parameters".
const READ_WINDOW = {
	maxSearchWindow: 10000,
	checkpointInterval: 10,
};

/**
 * Build a subscription filter from a projection's source definition.
 *
 * Matches the subscription spec at specs/subscription.md.
 */
export function buildSubscriptionFilter(
	info: ProjectionInfo,
): Filter | undefined {
	switch (info.source.type) {
		case "all":
			return buildEventTypeFilter(info);
		case "streams": {
			// fromCategory multi-arg puts $ce- streams in the streams array
			if (info.source.streams.every((s) => s.startsWith("$ce-"))) {
				const categories = info.source.streams.map((s) =>
					s.slice("$ce-".length),
				);
				return streamNameFilter({
					prefixes: categories.map((c) => `${c}-`),
					...READ_WINDOW,
				});
			}
			return streamNameFilter({
				regex: `^(${info.source.streams.map(escapeRegex).join("|")})$`,
				...READ_WINDOW,
			});
		}
		case "categories":
			return streamNameFilter({
				prefixes: info.source.categories.map((c) => `${c}-`),
				...READ_WINDOW,
			});
	}
}

function buildEventTypeFilter(info: ProjectionInfo): Filter | undefined {
	if (info.events === "all") return undefined;

	const prefixes = [...info.events];
	if (info.settings.handlesDeletedNotifications) {
		prefixes.push("$streamDeleted", "$metadata");
	}
	return eventTypeFilter({ prefixes, ...READ_WINDOW });
}

/**
 * Get the resolveLinks setting for a subscription based on engine version.
 * V1 uses false (raw $> events visible), V2 uses true (links always resolved).
 */
export function getResolveLinks(engineVersion: 1 | 2): boolean {
	return engineVersion === 2;
}

/** Escape special regex characters in a string. */
export const escapeRegex = (s: string) =>
	s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
