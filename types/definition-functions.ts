import type {
  FromAllChain,
  FromCategoryChain,
  FromStreamChain,
} from "./chains.ts";
import type { KurrentEvent } from "./events.ts";
import type { ProjectionOptions } from "./options.ts";
import type { State } from "./state.ts";

export interface OptionsFn {
  /**
   * Sets runtime options for the projection.
   *
   * @param projectionOptions Configuration object that customizes projection behavior
   *
   * @example
   * options({
   *   $includeLinks: true,
   *   reorderEvents: true,
   *   processingLag: 500
   * });
   */
  (projectionOptions: ProjectionOptions): void;
}

export interface FromStreamFn {
  /**
   * Selects events from a single stream for processing.
   *
   * @param streamId - The identifier of the stream to select events from
   *
   * @example
   * fromStream('cart-123')
   *   .when({
   *     $init: () => ({ items: [], total: 0 }),
   *     ItemAdded: (state, event) => ({
   *       items: [...state.items, event.body],
   *       total: state.total + event.body.price
   *     })
   *   });
   */
  <S extends State = State>(streamId: string): FromStreamChain<S>;
}

export interface FromCategoryFn {
  /**
   * Selects events from a category. In v1 this reads from the `$ce-{category}` stream
   * created by the $by_category system projection. In v2 this filters $all by stream prefix.
   *
   * Accepts a single category, an array, or variadic arguments (same as fromCategories).
   *
   * @example
   * fromCategory('order')
   *   .foreachStream()
   *   .when({
   *     $init: () => ({ total: 0 }),
   *     OrderPlaced: (state, event) => ({
   *       total: state.total + event.body.amount
   *     })
   *   });
   */
  <S extends State = State>(category: string): FromCategoryChain<S>;
  <S extends State = State>(categories: string[]): FromCategoryChain<S>;
  <S extends State = State>(...categories: string[]): FromCategoryChain<S>;
}

export interface FromCategoriesFn {
  /**
   * Selects events from multiple category streams.
   *
   * @param categories - Array of categories, or multiple category arguments
   *
   * @example
   * fromCategories(['electronics', 'books', 'clothing'])
   *   .when({ ... });
   *
   * @example
   * fromCategories('toys', 'sports', 'garden')
   *   .when({ ... });
   */
  <S extends State = State>(categories: string[]): FromCategoryChain<S>;
  <S extends State = State>(...categories: string[]): FromCategoryChain<S>;
}

export interface FromAllFn {
  /**
   * Selects events from the $all stream, which contains all events in the system.
   *
   * Note: The $all stream includes system events and may require filtering in your handlers.
   *
   * @example
   * fromAll()
   *   .when({
   *     $init: () => ({ eventCount: 0 }),
   *     $any: (state, event) => ({
   *       eventCount: state.eventCount + 1
   *     })
   *   });
   */
  <S extends State = State>(): FromAllChain<S>;
}

export interface FromStreamsFn {
  /**
   * Selects events from multiple named streams.
   *
   * @param streamIds - Array of stream identifiers
   *
   * @example
   * fromStreams(['user-123-orders', 'user-456-orders'])
   *   .when({
   *     OrderPlaced: (state, event) => ({
   *       totalOrders: state.totalOrders + 1
   *     })
   *   });
   */
  <S extends State = State>(streamIds: string[]): FromStreamChain<S>;
  <S extends State = State>(...streamIds: string[]): FromStreamChain<S>;
}

export interface OnEventFn {
  /**
   * Registers a handler for a specific event type. Alternative to the `when` chain syntax.
   *
   * @param eventName - The type of event to handle
   * @param eventHandler - Function that receives the current state and event, and returns the new state
   *
   * @example
   * on_event('OrderPlaced', (state, event) => ({
   *   totalOrders: state.totalOrders + 1,
   *   totalAmount: state.totalAmount + event.body.amount
   * }));
   */
  <S extends State = State>(
    eventName: string,
    eventHandler: (state: S, event: KurrentEvent) => S
  ): void;
}

export interface OnAnyFn {
  /**
   * Registers a handler for all events. Alternative to the `when` chain syntax with `$any`.
   *
   * @param eventHandler - Function that receives the current state and event, and returns the new state
   *
   * @example
   * on_any((state, event) => ({
   *   count: state.count + 1
   * }));
   */
  <S extends State = State>(
    eventHandler: (state: S, event: KurrentEvent) => S
  ): void;
}
