import type { KurrentEvent } from "./events.ts";
import type { Handlers } from "./handlers.ts";
import type { State } from "./state.ts";

export interface WhenFn<S extends State = State> {
  /**
   * Defines event handlers for the projection. Each handler receives the current state
   * and event, and returns the new state.
   *
   * @param handlers - Object containing event handlers and optional $init handler
   *
   * @example
   * .when({
   *   $init: () => ({ count: 0, total: 0 }),
   *   OrderPlaced: (state, event) => ({
   *     count: state.count + 1,
   *     total: state.total + event.body.amount
   *   }),
   *   OrderCancelled: (state, event) => ({
   *     count: state.count - 1,
   *     total: state.total - event.body.amount
   *   })
   * })
   */
  (handlers: Handlers<S>): WhenChain<S>;
}

export interface ForeachStreamFn<S extends State = State> {
  /**
   * Creates separate state instances for each input stream. Useful when you want
   * to process streams independently but with the same logic.
   *
   * @example
   * fromCategory('order')
   *   .foreachStream()
   *   .when({
   *     $init: () => ({ total: 0, count: 0 }),
   *     OrderPlaced: (state, event) => ({
   *       total: state.total + event.body.amount,
   *       count: state.count + 1
   *     })
   *   })
   */
  (): PartitionByChain<S>;
}

export interface PartitionByFn<S extends State = State> {
  /**
   * Defines how events should be partitioned for parallel processing. The partition key returned
   * determines which partition will handle the event. Non-string values are converted to strings.
   * Null, undefined, and empty strings are all converted to "".
   *
   * @param partitionKeyFn - Function that returns a partition key based on the event
   * @returns A chain object that allows defining event handlers for the partitioned stream
   * @throws {Error} May throw in future versions if returning null/undefined/empty string
   *
   * @example
   * // Partition by user ID
   * fromAll<UserState>()
   *   .partitionBy((event) => event.body.userId.toString())
   *   .when({...});
   *
   * @example
   * // Partition by category and ID
   * fromAll<OrderState>()
   *   .partitionBy((event) => `${event.body.category}-${event.body.id}`)
   *   .when({...});
   */
  (
    partitionKeyFn: (
      event: KurrentEvent
    ) => string | number | null | undefined
  ): PartitionByChain<S>;
}

export interface OutputStateFn<S extends State = State> {
  (): OutputStateChain<S>;
}

export interface TransformByFn<S extends State = State> {
  /**
   * Transforms the current state using the provided function. This is useful for
   * modifying the state structure or computing derived values.
   *
   * @param transformFn - Function that takes the current state and returns a new state
   * @returns Chain for further operations
   *
   * @example
   * // Transform order state to summary
   * .transformBy((state: OrderState) => ({
   *   orderCount: state.orders.length,
   *   totalAmount: state.orders.reduce((sum, order) => sum + order.amount, 0),
   *   lastOrderDate: state.orders[state.orders.length - 1]?.date
   * }))
   */
  (transformFn: (s: S) => S): TransformationChain<S>;
}

export interface FilterByFn<S extends State = State> {
  /**
   * Filters the state based on a predicate. If the predicate returns false,
   * the state becomes null and stops further processing in the chain.
   *
   * @param filterFn - Predicate function that determines if state should be kept
   * @returns Chain for further operations
   *
   * @example
   * // Only keep states with active orders
   * .filterBy((state: OrderState) => state.orders.some(order => order.status === 'active'))
   */
  (filterFn: (s: S) => boolean): TransformationChain<S>;
}

export interface OutputToFn {
  /**
   * Writes the projection state to a specified output stream.
   * For partitioned projections, supports template strings with {0}.
   *
   * @param outputStreamName - Name of the stream to write state to, can include {0} for partition name.
   * @param partitionResultStreamNamePattern - Name of the stream to write partition results to.
   *
   * @example
   * // Simple output
   * .outputTo('order-summaries')
   *
   * @example
   * // Partitioned output
   * .outputTo(`order-summaries`, 'user-{0}-summary')
   */
  (outputStreamName: string, partitionResultStreamNamePattern?: string): void;
}

interface ChainWithTransforms<S extends State = State> {
  /** Transforms the projection state according to the function provided.  */
  transformBy: TransformByFn<S>;

  /**
   * Filters the projection state according to the function provided.
   * If the filter function returns false, then the projection state will be transformed to null.
   * If the filter function returns true, then the projection state will remain unchanged.
   */
  filterBy: FilterByFn<S>;
}

interface ChainWithOutputTo {
  /**
   * Outputs the projection state to the specified stream.
   *
   * In the case of partitioned state, each state can be output to a different stream depending on the supplied template string.
   * If the projection is running in `Continuous` mode, the projection will create a Result event in the specified stream for each input event.
   * If the projection is running in `OneTime` mode, the projection will create a single Result event in the specified stream with the final state of the projection.
   */
  outputTo: OutputToFn;
}

interface ChainWithOutputState<S extends State = State> {
  /**
   * Causes a stream called `$projections-{projection-name}-result` to be produced with the state as the event body.
   *
   * If the projection is running in `Continuous` mode, the projection will create a Result event in the `$projections-{projection-name}-result` stream for each input event.
   * If the projection is running in `OneTime` mode, the projection will create a single Result event in the `$projections-{projection-name}-result` stream with the final state of the projection.
   */
  outputState: OutputStateFn<S>;
}

interface ChainWithWhen<S extends State = State> {
  /** Performs a fold operation across the events in the projection: each event is processed according to the specified handlers. */
  when: WhenFn<S>;
}

export interface ChainWithPartitionBy<S extends State = State> {
  /** Partitions the projection according to the specified partition function. The state received by `when` handlers will be a partition based on this function. */
  partitionBy: PartitionByFn<S>;
}

interface ChainWithForeachStream<S extends State = State> {
  /** Runs a projection pipeline (with separate state) for each input stream. */
  foreachStream: ForeachStreamFn<S>;
}

export type WhenChain<S extends State = State> = ChainWithTransforms<S> &
  ChainWithOutputTo &
  ChainWithOutputState<S>;

export type PartitionByChain<S extends State = State> = ChainWithWhen<S>;

export type OutputStateChain<S extends State = State> = ChainWithTransforms<S> &
  ChainWithOutputTo;

export type TransformationChain<S extends State = State> =
  ChainWithTransforms<S> & ChainWithOutputTo & ChainWithOutputState<S>;

export type FromStreamChain<S extends State = State> = ChainWithWhen<S> &
  ChainWithPartitionBy<S> &
  ChainWithOutputState<S>;

export type FromAllChain<S extends State = State> = FromStreamChain<S> &
  ChainWithForeachStream<S>;

export type FromCategoryChain<S extends State = State> = FromStreamChain<S> &
  ChainWithForeachStream<S>;
