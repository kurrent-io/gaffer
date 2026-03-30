import { State } from "./state.ts";

export type EventBody = Record<string, unknown>;

export type EventMetadata = Record<string, unknown>;

export interface KurrentEvent<S extends State = State> {
  streamId: string;

  eventType: string;

  /**
   * Return value of the partition function when using partitionBy.
   * When partitionBy is not being used, the partition will be an empty string.
   */
  partition: string;

  /** Event data. Synonymous with event.body. Only populated when the event data is JSON. */
  data?: S; // Now data will match the State type

  /** Event data. Synonymous with event.data. Only populated when the event data is JSON. */
  body?: S; // And body will match too

  /** JSON string of event data. */
  bodyRaw: string;

  /** Event metadata as a JS object. */
  metadata?: EventMetadata;

  /** JSON string of event metadata. */
  metadataRaw: string;

  /** When processing LinkTo events, this field stores the metadata
   * of the linkTo event while event.metadata stores the
   * linked event's metadata */
  linkMetadata?: string;

  /** LinkTo event's metadata as a JSON string. */
  linkMetadataRaw?: string;

  /** True when the event has a JSON body. if isJson is false, the event may have an undefined body. */
  isJson: boolean;

  /** Number of the event within its stream, a.k.a. the stream revision or version. */
  sequenceNumber: number;

  /** The unique identifier for the event. */
  eventId: string;
}

export interface LinkMetadata extends EventMetadata {
  /** The type of link being created (e.g., 'reference', 'backup', 'audit') */
  linkType: string;
  /** ISO timestamp when the link was created */
  createdAt?: string;
  /** Optional description of why this link was created */
  description?: string;
}

export interface StreamLinkMetadata extends EventMetadata {
  /** Reason for creating the stream link */
  reason?: string;
  /** ISO timestamp when the link was created */
  linkedAt?: string;
  /** Optional ISO timestamp when the link should no longer be considered valid */
  expiresAt?: string;
}
