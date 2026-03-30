import type {
  EventBody,
  EventMetadata,
  KurrentEvent,
  LinkMetadata,
  StreamLinkMetadata,
} from "./events.ts";
import { State } from "./state.ts";

export interface LogFn {
  /**
   * Appends an event to a stream. Can only be used within the handler of {@link when}.
   *
   * @param streamId - Specifies the stream to which events will be emitted.
   * @param eventType - Type of the emitted event.
   * @param eventBody - A JavaScript object representing the JSON body of the emitted event.
   * @param metadata - A JavaScript object representing the JSON metadata of the emitted event.
   * @param expectedVersion - Optional expected version for optimistic concurrency.
   * @throws {Error} If the stream version doesn't match expectedVersion.
   * @throws {Error} If called outside a projection handler.
   * @returns {void}
   *
   * @example
   * log(
   *   'purchase-123',
   *   'PurchaseCompleted',
   *   { orderId: '123', total: 99.99 },
   *   { userId: 'user-456' }
   * );
   */
  (
    streamId: string,
    eventType: string,
    eventBody: EventBody,
    metadata: EventMetadata,
    expectedVersion?: number
  ): void;
}

export interface EmitFn {
  /**
   * Appends an event to a stream. Can only be used within the handler of {@link when}.
   *
   * @param streamId - Specifies the stream to which events will be emitted.
   * @param eventType - Type of the emitted event.
   * @param eventBody - A JavaScript object representing the JSON body of the emitted event.
   * @param metadata - A JavaScript object representing the JSON metadata of the emitted event.
   * @throws {Error} If called outside a projection handler.
   * @throws {Error} If eventBody cannot be serialized to JSON.
   * @throws {Error} If streamId is empty or invalid.
   * @throws {Error} If eventType is empty or invalid.
   *
   * @example
   * // Emit a purchase completed event
   * emit(
   *   'order-123',
   *   'PurchaseCompleted',
   *   { orderId: '123', total: 99.99 },
   *   { userId: 'user-456' }
   * );
   *
   * @example
   * // Emit a user profile updated event
   * emit(
   *   'user-456',
   *   'ProfileUpdated',
   *   {
   *     name: 'John Doe',
   *     email: 'john@example.com'
   *   },
   *   {
   *     updatedBy: 'admin',
   *     timestamp: '2025-06-17T04:00:00Z'
   *   }
   * );
   *
   * @example
   * try {
   *   emit(
   *     'order-123',
   *     'PurchaseCompleted',
   *     { orderId: '123', total: 99.99 },
   *     { userId: 'user-456' }
   *   );
   * } catch (error) {
   *   // Handle invalid stream ID, event type, or JSON serialization errors
   *   log(`Failed to emit PurchaseCompleted: ${error.message}`);
   * }
   */
  (
    streamId: string,
    eventType: string,
    eventBody: EventBody,
    metadata: EventMetadata
  ): void;
}

export interface LinkToFn {
  /**
   * Appends a LinkTo event to a stream. Can only be used within the handler of {@link when}.
   *
   * @param streamId - Specifies the stream to which the LinkTo event will be emitted.
   * @param event - Event which will be linked.
   * @param metadata - A JavaScript object representing the JSON metadata of the LinkTo event.
   * @throws {Error} If called outside a projection handler.
   * @throws {Error} If streamId is empty or invalid.
   * @throws {Error} If event is null or undefined.
   * @throws {Error} If metadata lacks required linkType.
   *
   * @example
   * // Link to a purchase event in an order summary stream
   * linkTo(
   *   'order-summary-123',
   *   purchaseEvent, // KurrentEvent<PurchaseState>
   *   { linkType: 'purchase-reference' }
   * );
   *
   * @example
   * // Link to a user activity event in a user timeline
   * interface UserState {
   *   userId: string;
   *   action: string;
   *   timestamp: string;
   * }
   *
   * linkTo(
   *   'user-456-timeline',
   *   userActivityEvent, // KurrentEvent<UserState>
   *   {
   *     linkType: 'activity-reference',
   *     createdAt: '2025-06-17T04:00:00Z'
   *   }
   * );
   *
   * @example
   * try {
   *   linkTo(
   *     'order-summary-123',
   *     purchaseEvent,
   *     {
   *       linkType: 'purchase-reference',
   *       createdAt: new Date().toISOString()
   *     }
   *   );
   * } catch (error) {
   *   log(`Failed to create link: ${error.message}`);
   * }
   */
  <S extends State = State>(
    streamId: string,
    event: KurrentEvent<S>,
    metadata: LinkMetadata
  ): void;
}

export interface LinkStreamToFn {
  /**
   * Appends a StreamReference event to a stream. Can only be used within the handler of {@link when}.
   *
   * @param streamId - Specifies the stream to which the StreamReference event will be emitted.
   * @param linkedStreamId - Specifies the stream to be linked.
   * @param metadata - Optional metadata for the stream link.
   * @throws {Error} If called outside a projection handler.
   * @throws {Error} If either streamId or linkedStreamId is empty/invalid.
   * @throws {Error} If trying to create a circular reference.
   *
   * @example
   * // Link streams with metadata
   * linkStreamTo(
   *   'user-456-profile',
   *   'user-456-orders',
   *   {
   *     reason: 'Associated user orders',
   *     linkedAt: new Date().toISOString()
   *   }
   * );
   */
  (
    streamId: string,
    linkedStreamId: string,
    metadata?: StreamLinkMetadata
  ): void;
}

export interface CopyToFn {
  /**
   * Creates a copy of events from one stream to another.
   *
   * @param streamId - Specifies the destination stream where events will be copied to.
   * @param sourceStreamId - Specifies the source stream to copy events from.
   * @throws {Error} If called outside a projection handler.
   * @throws {Error} If source stream doesn't exist.
   * @throws {Error} If destination stream is invalid.
   * @throws {Error} If trying to copy to the same stream.
   *
   * @example
   * // Copy events from a temporary cart to an order stream
   * copyTo(
   *   'order-123',
   *   'temp-cart-456'
   * );
   *
   * @example
   * // Create a backup copy of a user's profile stream
   * copyTo(
   *   'user-456-profile-backup',
   *   'user-456-profile'
   * );
   *
   * @example
   * // Copy events to an audit stream
   * copyTo(
   *   'audit-stream-2025',
   *   'sensitive-operations-stream'
   * );
   */
  (streamId: string, sourceStreamId: string): void;
}
