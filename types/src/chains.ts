import type { KurrentEvent } from "./events.ts";
import type { BiStateHandlerFor, HandlerFor } from "./handlers.ts";
import type { State } from "./state.ts";

export interface WhenFn<S extends State = State> {
	// BiState overload: requires `$initShared`. Returns a chain whose
	// fold state is the tuple `[S, TShared]`, so any subsequent
	// transformBy / filterBy receives the tuple - the runtime passes
	// the array straight in. Contextual typing for `[state, shared]`
	// handler parameters does NOT resolve `TShared` (TS resolves
	// generics after the literal is typed), so users will need an
	// explicit annotation on the destructured tuple. See
	// types/tests/handlers for the pattern.
	<
		TShared extends State,
		H extends { $initShared: () => TShared } & {
			[K in keyof H]: BiStateHandlerFor<S, TShared, K & string>;
		},
	>(
		handlers: H,
	): WhenChain<readonly [S, TShared]>;
	/**
	 * Defines event handlers for the projection. Each handler receives
	 * the current state and event, and returns the new state.
	 *
	 * @param handlers - Object keyed by event type. Built-in keys
	 *   (`$init`, `$any`, `$created`, `$deleted`) get their specific
	 *   signatures; everything else is treated as a regular event
	 *   handler `(state, event) => state`.
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
	<H extends { [K in keyof H]: HandlerFor<S, K & string> }>(handlers: H): WhenChain<S>;
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
	 * Splits the projection state by the key returned from the function: each
	 * distinct key gets its own state instance, processed independently. Numeric
	 * keys are coerced to strings. Returning `null` or `undefined` skips the
	 * event for partitioning - it won't be processed by `when` handlers on this
	 * chain.
	 *
	 * @param partitionKeyFn - Function that returns a partition key based on the event
	 * @returns A chain that accepts `when` to define handlers operating on the
	 *   partitioned state.
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
	(partitionKeyFn: (event: KurrentEvent) => string | number | null | undefined): PartitionByChain<S>;
}

export interface OutputStateFn<S extends State = State> {
	(): OutputStateChain<S>;
}

export interface TransformByFn<S extends State = State> {
	/**
	 * Maps the projection state to a different shape for the *output*. Does
	 * NOT affect the state seen by the `when` handlers - they continue to
	 * receive the original `S`. Useful for projecting only a subset of state
	 * to the result stream, or computing derived values.
	 *
	 * @param transformFn - Maps the current state to a derived shape.
	 * @returns Chain that emits the derived shape via `outputTo` / `outputState`.
	 *
	 * @example
	 * // Output only a summary view of the projection state
	 * .transformBy((state: OrderState) => ({
	 *   orderCount: state.orders.length,
	 *   totalAmount: state.orders.reduce((sum, order) => sum + order.amount, 0),
	 *   lastOrderDate: state.orders[state.orders.length - 1]?.date
	 * }))
	 */
	<R extends State = S>(transformFn: (s: S) => R): TransformationChain<R>;
}

export interface FilterByFn<S extends State = State> {
	/**
	 * Suppresses output when the predicate returns false: the projection's
	 * result for the current event becomes `null` instead of the state. Does
	 * NOT halt processing - subsequent events still flow through the
	 * `when` handlers and accumulate state. Use to gate which events
	 * produce output, not to stop the projection.
	 *
	 * @param filterFn - Returns `true` to emit the state, `false` to emit null.
	 * @returns Chain for further `transformBy` / `filterBy` / `outputTo`.
	 *
	 * @example
	 * // Only emit results for shopping carts that still have active orders
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
	/** Defines event handlers for the projection. Each handler receives the current state and event, and returns the new state. */
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

export type WhenChain<S extends State = State> = ChainWithTransforms<S> & ChainWithOutputTo & ChainWithOutputState<S>;

export type PartitionByChain<S extends State = State> = ChainWithWhen<S>;

export type OutputStateChain<S extends State = State> = ChainWithTransforms<S> & ChainWithOutputTo;

export type TransformationChain<S extends State = State> = ChainWithTransforms<S> &
	ChainWithOutputTo &
	ChainWithOutputState<S>;

export type FromStreamChain<S extends State = State> = ChainWithWhen<S> &
	ChainWithPartitionBy<S> &
	ChainWithOutputState<S>;

export type FromAllChain<S extends State = State> = FromStreamChain<S> & ChainWithForeachStream<S>;

export type FromCategoryChain<S extends State = State> = FromStreamChain<S> & ChainWithForeachStream<S>;
