export type EventBody = Record<string, unknown>;

export type EventMetadata = Record<string, unknown>;

/**
 * An event seen by a projection handler. `TBody` parameterises the
 * shape of `body` / `data`; when omitted it defaults to the open
 * `EventBody` (`Record<string, unknown>`). Specialise it for
 * standalone helpers or test fixtures that know the body shape:
 *
 * @example
 * ```ts
 * function totalForOrder(event: Projection.KurrentEvent<{ cents: number }>) {
 *   return event.body?.cents ?? 0;
 * }
 * ```
 */
export interface KurrentEvent<TBody extends EventBody = EventBody> {
	/** Source stream the event belongs to. */
	streamId: string;

	/** Event type name (the second argument passed to `emit`). */
	eventType: string;

	/**
	 * Return value of the partition function when using partitionBy.
	 * When partitionBy is not being used, the partition will be an empty string.
	 */
	partition: string;

	/** Stream category extracted from the streamId (prefix before the first `-`). */
	category: string;

	/**
	 * Parsed JSON event body. Null when the event body is not JSON.
	 * This is the primary property for accessing event data.
	 */
	body: TBody | null;

	/**
	 * @deprecated Use `body` instead. `data` is retained as an alias
	 *   for older projections; new projections should use `body`.
	 */
	data: TBody | null;

	/** JSON string of the event body. Null when zero-length. */
	bodyRaw: string | null;

	/** Parsed event metadata. Null when metadata is empty or zero-length. */
	metadata: EventMetadata | null;

	/** JSON string of event metadata. Null when zero-length. */
	metadataRaw: string | null;

	/**
	 * When processing LinkTo events, the parsed metadata of the linkTo
	 * event itself (the source / outer event), while `metadata` stores the
	 * linked event's metadata. Null when not processing a link.
	 */
	linkMetadata: EventMetadata | null;

	/** JSON string of the linkTo event's metadata. Null when not a link. */
	linkMetadataRaw: string | null;

	/** True when the event body is JSON. When false, `body` is null. */
	isJson: boolean;

	/** Number of the event within its stream (a.k.a. the stream revision). */
	sequenceNumber: number;

	/** Unique identifier for the event. */
	eventId: string;

	/** ISO 8601 datetime of event creation. */
	created: string;
}
