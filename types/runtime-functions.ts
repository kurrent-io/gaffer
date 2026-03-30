import type { EventBody, EventMetadata, KurrentEvent } from "./events.ts";

export interface LogFn {
  /**
   * Logs a message to the projection log. Behaves like console.log.
   * Can only be used within the handler of {@link when}.
   *
   * @example
   * log('Processing event', event.eventType, event.streamId);
   */
  (...args: unknown[]): void;
}

export interface EmitFn {
  /**
   * Appends an event to a stream. Can only be used within the handler of {@link when}.
   *
   * @param streamId - Specifies the stream to which events will be emitted.
   * @param eventType - Type of the emitted event.
   * @param eventBody - A JavaScript object representing the JSON body of the emitted event.
   * @param metadata - Optional metadata for the emitted event.
   *
   * @example
   * emit(
   *   'order-123',
   *   'PurchaseCompleted',
   *   { orderId: '123', total: 99.99 },
   *   { userId: 'user-456' }
   * );
   *
   * @example
   * emit(
   *   'user-456',
   *   'ProfileUpdated',
   *   { name: 'John Doe', email: 'john@example.com' }
   * );
   */
  (
    streamId: string,
    eventType: string,
    eventBody: EventBody,
    metadata?: EventMetadata
  ): void;
}

export interface LinkToFn {
  /**
   * Appends a LinkTo event to a stream. Can only be used within the handler of {@link when}.
   *
   * @param streamId - Specifies the stream to which the LinkTo event will be emitted.
   * @param event - Event which will be linked.
   * @param metadata - Optional metadata for the LinkTo event.
   *
   * @example
   * linkTo('order-summary-123', event);
   *
   * @example
   * linkTo('user-timeline', event, { source: 'activity-projection' });
   */
  (
    streamId: string,
    event: KurrentEvent,
    metadata?: EventMetadata
  ): void;
}

export interface LinkStreamToFn {
  /**
   * Appends a StreamReference event to a stream. Can only be used within the handler of {@link when}.
   *
   * @param streamId - Specifies the stream to which the StreamReference event will be emitted.
   * @param linkedStreamId - Specifies the stream to be linked.
   * @param metadata - Optional metadata for the stream link.
   *
   * @example
   * linkStreamTo('user-456-profile', 'user-456-orders');
   */
  (
    streamId: string,
    linkedStreamId: string,
    metadata?: EventMetadata
  ): void;
}

export interface CopyToFn {
  /**
   * Copies events to a stream. Currently a stub in the runtime.
   * Can only be used within the handler of {@link when}.
   *
   * @param streamId - Destination stream.
   * @param eventType - Type of the copied event.
   * @param eventBody - Body of the copied event.
   */
  (streamId: string, eventType: string, eventBody: EventBody): void;
}
