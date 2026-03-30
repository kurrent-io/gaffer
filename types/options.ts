export interface ProjectionOptions {
  /**
   * Controls whether event links are included in projection results.
   * When enabled, maintains references and relationships between
   * events and their related streams.
   *
   * @remarks
   * - Preserves event relationships in projection outputs
   * - Enables navigation between linked events and streams
   * - Helps maintain event correlation context
   * - May increase storage requirements and processing overhead
   * - Essential for tracking event causality chains
   *
   * @example
   * ```typescript
   * const options = {
   *   $includeLinks: true, // Include event links
   *   // ... other options
   * };
   * ```
   *
   * @note
   * Enable this option when you need to maintain relationships between
   * events or track event causality. Consider the storage overhead as
   * including links increases the size of projection results.
   *
   * @default false
   */
  $includeLinks?: boolean;

  /**
   * Specifies the name of the stream where projection results will be stored.
   * Used to define a dedicated output stream for collecting and accessing
   * projection results.
   *
   * @remarks
   * - Provides a single destination for projection outputs
   * - Enables easy access to projection results
   * - Must be unique across your event store
   * - Can be used for monitoring and debugging
   * - Supports forward and backward result reading
   *
   * @example
   * ```typescript
   * const options = {
   *   resultStreamName: "my-projection-results",
   *   // ... other options
   * };
   * ```
   *
   * @note
   * Choose a descriptive and unique name that clearly identifies the projection
   * and its purpose. Consider your stream naming conventions and ensure the
   * name doesn't conflict with existing streams.
   *
   * @default undefined
   */
  resultStreamName?: string | null;

  /**
   * Enables automatic reordering of events based on their timestamps.
   * When enabled, ensures events are processed in their actual temporal order
   * regardless of arrival sequence.
   *
   * @remarks
   * - Can only be used for multi-stream projections (`fromCategory` or `fromStreams`).
   * - You must set a `processingLag` of at least 50ms, otherwise you will get an error when you try to save the projection.
   * - Maintains causal consistency by processing events in correct temporal order
   * - Buffers out-of-order events until their preceding events arrive
   * - May introduce additional latency due to buffering and sorting
   * - Particularly useful in distributed systems or with multiple event sources
   * - Helps prevent state inconsistencies from misordered event processing
   *
   * @example
   * ```typescript
   * const options = {
   *   reorderEvents: true, // Enable event reordering
   *   // ... other options
   * };
   * ```
   *
   * @note
   * Consider the trade-off between consistency and latency when enabling this option.
   * The additional buffering required for reordering may impact processing throughput
   * and memory usage.
   *
   * @default false
   */
  reorderEvents?: boolean;

  /**
   * How long to wait before processing an event when using event reordering.
   *
   * @remarks
   * - Should only be used when `reorderEvents` is `true`.
   * - You must set to a minimum of 50ms, otherwise you will get an error when you try to save the projection.
   *
   * @example
   * ```typescript
   * const options = {
   *   reorderEvents: true,
   *   processingLag: 5000, // 5 seconds lag
   *   // ... other options
   * };
   * ```
   * @default undefined
   */
  processingLag?: number;

  /**
   * Enables bi-state mode for the projection.
   * When enabled, the projection maintains both individual and shared states.
   *
   * @remarks
   * - Maintains both individual and shared state contexts
   * - Returns state as [individualState, sharedState] array
   * - Requires initialization of both individual and shared states
   * - Incompatible with delete notifications
   * - Useful for projections that need to track both local and global state
   *
   * @example
   * ```typescript
   * const options = {
   *   biState: true,
   *   // ... other options
   * };
   * ```
   *
   * @note
   * When using bi-state mode, ensure both states are properly initialized
   * and handle state updates appropriately in your projection logic.
   *
   * @default false
   */
  biState?: boolean;
}
