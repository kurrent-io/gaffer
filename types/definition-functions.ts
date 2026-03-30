import type { FromCategoryChain, FromStreamChain } from "./chains.ts";
import { KurrentEvent } from "./events.ts";
import type { ProjectionOptions } from "./options.ts";
import type { State } from "./state.ts";

export interface OptionsFn {
  /**
   * Sets runtime options for the projection. These options control how events are processed
   * and how the projection behaves.
   *
   * @param projectionOptions Configuration object that customizes projection behavior
   *
   * @example
   * // Configure a projection to process linked events
   * options({
   *   $includeLinks: true,
   *   reorderEvents: false
   * });
   *
   *
   * @example
   * // Configure a projection with custom result stream
   * options({
   *   resultStreamName: 'daily-order-summary',
   * });
   *
   * @example
   * // Set up a projection with bi-state processing
   * options({
   *   biState: true,
   *   processingLag: 1000
   * });
   *
   * @see {@link ProjectionOptions} for detailed description of all available options:
   * - resultStreamName: Name for the result stream
   * - $includeLinks: Process linked events
   * - reorderEvents: Enable event reordering, can only be used for multi-stream projections (`fromCategory` or `fromStreams`).
   * - processingLag: Processing delay in ms
   * - biState: Enable bi-state processing
   */
  (projectionOptions: ProjectionOptions): void;
}

export interface FromStreamFn {
  /**
   * Selects events from a single stream for processing in a projection. This is useful when you want
   * to process all events from a specific stream, like tracking the state of a single entity.
   *
   * @param streamId - The identifier of the stream to select events from (e.g., 'order-123', 'user-456')
   * @returns A chain object that allows further operations on the selected stream
   *
   * @example
   * // Track the state of a shopping cart
   * interface CartState {
   *   items: Array<{
   *     productId: string;
   *     quantity: number;
   *     price: number;
   *   }>;
   *   totalAmount: number;
   * }
   *
   * fromStream('cart-123')
   *   .when({
   *     $init: () => ({
   *       items: [],
   *       totalAmount: 0
   *     }),
   *     ItemAdded: (state: CartState, event) => ({
   *       items: [...state.items, event.body],
   *       totalAmount: state.totalAmount + (event.body.quantity * event.body.price)
   *     }),
   *     ItemRemoved: (state: CartState, event) => ({
   *       items: state.items.filter(item => item.productId !== event.body.productId),
   *       totalAmount: state.totalAmount - (
   *         state.items.find(item => item.productId === event.body.productId)?.quantity *
   *         state.items.find(item => item.productId === event.body.productId)?.price || 0
   *       )
   *     })
   *   });
   *
   * @example
   * // Monitor user preferences
   * interface UserPreferences {
   *   theme: 'light' | 'dark';
   *   notifications: boolean;
   *   language: string;
   *   lastUpdated: string;
   * }
   *
   * fromStream('user-456-preferences')
   *   .when({
   *     $init: () => ({
   *       theme: 'light',
   *       notifications: true,
   *       language: 'en',
   *       lastUpdated: ''
   *     }),
   *     PreferencesUpdated: (state: UserPreferences, event) => ({
   *       ...state,
   *       ...event.body,
   *       lastUpdated: event.metadata.timestamp
   *     })
   *   });
   */
  <S extends State = State>(streamId: string): FromStreamChain<S>;
}

export interface FromCategoryFn {
  /**
   * Selects events from a single category stream (`$ce-{category}`). The category stream is automatically
   * created by the $by_category system projection and contains all events from streams in that category.
   *
   * @param category - The category to select events from (e.g., 'user', 'order', 'product')
   * @returns A chain object that allows further operations on the category stream
   *
   * @example
   * // Track order statistics for a category
   * interface OrderStats {
   *   totalOrders: number;
   *   totalAmount: number;
   *   lastOrderTime: string;
   * }
   *
   * fromCategory('order')
   *   .when({
   *     $init: () => ({
   *       totalOrders: 0,
   *       totalAmount: 0,
   *       lastOrderTime: ''
   *     }),
   *     OrderPlaced: (state: OrderStats, event) => ({
   *       totalOrders: state.totalOrders + 1,
   *       totalAmount: state.totalAmount + event.body.amount,
   *       lastOrderTime: event.metadata.timestamp
   *     })
   *   });
   *
   * @example
   * // Monitor user activity in a category
   * interface UserActivity {
   *   activeUsers: Set<string>;
   *   activityCount: number;
   * }
   *
   * fromCategory('user')
   *   .partitionBy(event => event.body.userId)
   *   .when({
   *     $init: () => ({
   *       activeUsers: new Set(),
   *       activityCount: 0
   *     }),
   *     UserLoggedIn: (state: UserActivity, event) => ({
   *       activeUsers: state.activeUsers.add(event.body.userId),
   *       activityCount: state.activityCount + 1
   *     })
   *   });
   */
  <S extends State = State>(category: string): FromCategoryChain<S>;
}

export interface FromCategoriesFn {
  /**
   * Selects events from multiple category streams. Each category stream (`$ce-{category}`)
   * is created by the $by_category system projection and contains all events from streams
   * in that category.
   *
   * @param categories - Array of categories to select events from, or multiple category arguments
   * @returns A chain object that allows further operations on the selected category streams
   *
   * @example
   * // Monitor events from multiple product categories
   * interface ProductStats {
   *   categories: Record<string, {
   *     eventCount: number;
   *     lastUpdated: string;
   *   }>;
   * }
   *
   * fromCategories(['electronics', 'books', 'clothing'])
   *   .when({
   *     $init: () => ({
   *       categories: {}
   *     }),
   *     ProductUpdated: (state: ProductStats, event) => ({
   *       categories: {
   *         ...state.categories,
   *         [event.metadata.category]: {
   *           eventCount: (state.categories[event.metadata.category]?.eventCount || 0) + 1,
   *           lastUpdated: event.metadata.timestamp
   *         }
   *       }
   *     })
   *   });
   *
   * @example
   * // Track inventory across departments
   * interface InventoryState {
   *   departments: Record<string, number>;
   * }
   *
   * fromCategories('toys', 'sports', 'garden')  // Can also pass categories as arguments
   *   .when({
   *     InventoryChanged: (state: InventoryState, event) => ({
   *       departments: {
   *         ...state.departments,
   *         [event.metadata.category]: event.body.newQuantity
   *       }
   *     })
   *   });
   */
  <S extends State = State>(categories: string[]): FromCategoryChain<S>;
  <S extends State = State>(...categories: string[]): FromCategoryChain<S>;
}

export interface FromAllFn {
  /**
   * Selects events from the $all stream, which contains all events in the system in their original order.
   * This is useful for creating projections that need to process all events regardless of their stream.
   *
   * Note: The $all stream includes system events and may require additional filtering in your handlers.
   *
   * @returns A chain object that allows further operations on the $all stream
   *
   * @example
   * // Global event statistics
   * interface GlobalStats {
   *   eventCount: number;
   *   eventTypes: Record<string, number>;
   *   lastEventTime: string;
   * }
   *
   * fromAll()
   *   .when({
   *     $init: () => ({
   *       eventCount: 0,
   *       eventTypes: {},
   *       lastEventTime: ''
   *     }),
   *     $any: (state: GlobalStats, event) => ({
   *       eventCount: state.eventCount + 1,
   *       eventTypes: {
   *         ...state.eventTypes,
   *         [event.eventType]: (state.eventTypes[event.eventType] || 0) + 1
   *       },
   *       lastEventTime: event.metadata.timestamp
   *     })
   *   });
   *
   * @example
   * // System-wide activity monitor
   * interface ActivityMonitor {
   *   activeStreams: Set<string>;
   *   lastActivity: Record<string, string>;
   * }
   *
   * fromAll()
   *   .when({
   *     $init: () => ({
   *       activeStreams: new Set(),
   *       lastActivity: {}
   *     }),
   *     $any: (state: ActivityMonitor, event) => ({
   *       activeStreams: state.activeStreams.add(event.streamId),
   *       lastActivity: {
   *         ...state.lastActivity,
   *         [event.streamId]: event.metadata.timestamp
   *       }
   *     })
   *   });
   *
   * @example
   * // Event timeline
   * interface TimelineEntry {
   *   timestamp: string;
   *   streamId: string;
   *   eventType: string;
   *   metadata: Record<string, any>;
   * }
   *
   * fromAll()
   *   .when({
   *     $init: () => ({ entries: [] as TimelineEntry[] }),
   *     $any: (state, event) => ({
   *       entries: [
   *         ...state.entries,
   *         {
   *           timestamp: event.metadata.timestamp,
   *           streamId: event.streamId,
   *           eventType: event.eventType,
   *           metadata: event.metadata
   *         }
   *       ].slice(-100) // Keep last 100 events
   *     })
   *   });
   */
  <S extends State = State>(): FromCategoryChain<S>;
}

export interface FromStreamsFn {
  /**
   * Selects events from multiple streams for processing in a projection. This allows you to
   * aggregate or process events from a specific set of streams together.
   *
   * @param streamIds - Array of stream identifiers to select events from
   * @returns A chain object that allows further operations on the selected streams
   *
   * @example
   * // Aggregate orders from multiple user streams
   * interface OrderSummary {
   *   totalOrders: number;
   *   ordersByUser: Record<string, number>;
   * }
   *
   * fromStreams(['user-123-orders', 'user-456-orders'])
   *   .when({
   *     OrderPlaced: (state: OrderSummary, event) => ({
   *       totalOrders: state.totalOrders + 1,
   *       ordersByUser: {
   *         ...state.ordersByUser,
   *         [event.streamId]: (state.ordersByUser[event.streamId] || 0) + 1
   *       }
   *     })
   *   });
   *
   * @example
   * // Monitor multiple product inventories
   * interface InventoryState {
   *   products: Record<string, number>;
   *   lowStockThreshold: number;
   * }
   *
   * fromStreams(['product-a-stock', 'product-b-stock', 'product-c-stock'])
   *   .when({
   *     StockUpdated: (state: InventoryState, event) => ({
   *       ...state,
   *       products: {
   *         ...state.products,
   *         [event.streamId]: event.body.quantity
   *       }
   *     })
   *   });
   *
   * @example
   * // Track activities across multiple user sessions
   * interface SessionActivity {
   *   activeSessions: string[];
   *   activityCount: Record<string, number>;
   * }
   *
   * fromStreams(['session-789', 'session-101', 'session-202'])
   *   .when({
   *     SessionStarted: (state: SessionActivity, event) => ({
   *       activeSessions: [...state.activeSessions, event.streamId],
   *       activityCount: state.activityCount
   *     }),
   *     ActivityLogged: (state: SessionActivity, event) => ({
   *       activeSessions: state.activeSessions,
   *       activityCount: {
   *         ...state.activityCount,
   *         [event.streamId]: (state.activityCount[event.streamId] || 0) + 1
   *       }
   *     })
   *   });
   */
  <S extends State = State>(streamIds: string[]): FromStreamChain<S>;
}

export interface OnEventFn {
  /**
   * Registers a handler for a specific event type in the selected streams. Can only be used within a projection definition.
   * The handler will be called only for events matching the specified event type.
   *
   * @param eventName - The type of event to handle (e.g., 'OrderPlaced', 'UserRegistered')
   * @param eventHandler - Function that receives the current state and event, and returns the new state
   *
   * @example
   * // Track order totals
   * interface OrderState {
   *   totalOrders: number;
   *   totalAmount: number;
   * }
   *
   * on_event('OrderPlaced', (state: OrderState, event) => {
   *   return {
   *     totalOrders: state.totalOrders + 1,
   *     totalAmount: state.totalAmount + event.body.amount
   *   };
   * });
   *
   * @example
   * // Maintain user profile
   * interface UserProfileState {
   *   email?: string;
   *   name?: string;
   *   lastUpdated?: string;
   * }
   *
   * on_event('ProfileUpdated', (state: UserProfileState, event) => {
   *   return {
   *     ...state,
   *     ...event.body,
   *     lastUpdated: event.metadata.timestamp
   *   };
   * });
   *
   * @example
   * // Track inventory levels
   * interface InventoryState {
   *   productId: string;
   *   quantity: number;
   *   reservations: number;
   * }
   *
   * on_event('ItemRestocked', (state: InventoryState, event) => {
   *   return {
   *     ...state,
   *     quantity: state.quantity + event.body.amount
   *   };
   * });
   */
  <S extends State = State>(
    eventName: string,
    eventHandler: (state: S, event: KurrentEvent<S>) => S
  ): void;
}

export interface OnAnyFn {
  /**
   * Registers a handler for all events in the selected streams. Can only be used within a projection definition.
   * The handler will be called for every event, regardless of its type.
   *
   * @param eventHandler - Function that receives the current state and event, and returns the new state
   *
   * @example
   * // Track all events in a counter
   * on_any((state: { count: number }, event) => {
   *   return { count: state.count + 1 };
   * });
   *
   * @example
   * // Log all events with their types
   * interface LogState {
   *   events: Array<{ type: string; data: any }>;
   * }
   *
   * on_any((state: LogState, event) => {
   *   return {
   *     events: [
   *       ...state.events,
   *       { type: event.eventType, data: event.body }
   *     ]
   *   };
   * });
   *
   * @example
   * // Maintain an audit trail of all events
   * interface AuditState {
   *   trail: Array<{
   *     eventType: string;
   *     streamId: string;
   *     timestamp: string;
   *   }>;
   * }
   *
   * on_any((state: AuditState, event) => {
   *   return {
   *     trail: [
   *       ...state.trail,
   *       {
   *         eventType: event.eventType,
   *         streamId: event.streamId,
   *         timestamp: event.metadata.timestamp
   *       }
   *     ]
   *   };
   * });
   */
  <S extends State = State>(
    eventHandler: (state: S, event: KurrentEvent<S>) => S
  ): void;
}
