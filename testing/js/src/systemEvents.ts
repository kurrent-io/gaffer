import type { TestEvent } from "./schemas.js";

/** Helpers for constructing KurrentDB system events. */
export const systemEvents = {
	/** Create a hard-delete event for a stream. Triggers `$deleted` handlers. */
	streamDeleted(streamId: string, sequenceNumber: number): TestEvent {
		return {
			eventType: "$streamDeleted",
			streamId,
			sequenceNumber,
			isJson: true,
			data: { streamId },
		};
	},
};
