/**
 * Configuration object for the `options()` global. Sets runtime
 * behaviour flags for the projection.
 */
export interface ProjectionOptions {
	/**
	 * Include LinkTo events as input alongside the originating event.
	 *
	 * @deprecated V1 engine only - has no effect under V2.
	 * @default false
	 */
	$includeLinks?: boolean;

	/** Name of the stream where projection results will be stored. */
	resultStreamName?: string;

	/**
	 * Reorder events by timestamp before processing. Requires
	 * `processingLag` >= 50ms. Can only be used with multi-stream
	 * projections (`fromStreams`).
	 *
	 * @deprecated V1 engine only - has no effect under V2.
	 * @default false
	 */
	reorderEvents?: boolean;

	/**
	 * Delay in ms before processing an event when `reorderEvents` is
	 * enabled. Minimum 50ms.
	 *
	 * @deprecated V1 engine only - has no effect under V2.
	 */
	processingLag?: number;

	/**
	 * Enable bi-state mode: the projection maintains both individual and
	 * shared states, initialized via `$init` and `$initShared`. Auto-enabled
	 * when `$initShared` is present in the handler set, so most projections
	 * don't need to set this explicitly.
	 *
	 * @default false
	 */
	biState?: boolean;
}
