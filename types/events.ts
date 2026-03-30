export type EventBody = Record<string, unknown>;

export type EventMetadata = Record<string, unknown>;

export interface KurrentEvent {
  streamId: string;

  eventType: string;

  /**
   * Return value of the partition function when using partitionBy.
   * When partitionBy is not being used, the partition will be an empty string.
   */
  partition: string;

  /** Stream category extracted from the streamId. */
  category: string;

  /**
   * Event data. Synonymous with event.body. Only populated when the event data is JSON.
   * @deprecated Use body instead.
   */
  data?: EventBody | null;

  /** Event data. Synonymous with event.data. Only populated when the event data is JSON. */
  body?: EventBody | null;

  /** JSON string of event data. Null when event data is zero-length. */
  bodyRaw: string | null;

  /** Event metadata as a JS object. Null when metadata is empty or zero-length. */
  metadata?: EventMetadata | null;

  /** JSON string of event metadata. Null when metadata is zero-length. */
  metadataRaw: string | null;

  /**
   * When processing LinkTo events, this field stores the parsed metadata
   * of the linkTo event while event.metadata stores the linked event's metadata.
   */
  linkMetadata?: EventMetadata | null;

  /** LinkTo event's metadata as a JSON string. */
  linkMetadataRaw?: string | null;

  /** True when the event has a JSON body. If isJson is false, body may be undefined. */
  isJson: boolean;

  /** Number of the event within its stream, a.k.a. the stream revision or version. */
  sequenceNumber: number;

  /** The unique identifier for the event. */
  eventId: string;

  /** ISO 8601 datetime when the event was created. */
  created: string;
}
