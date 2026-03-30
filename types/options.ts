export interface ProjectionOptions {
  /**
   * Include event links in projection results.
   * V1 only - has no effect in v2.
   * @default false
   */
  $includeLinks?: boolean;

  /** Name of the stream where projection results will be stored. */
  resultStreamName?: string;

  /**
   * Reorder events by timestamp before processing. Requires `processingLag` >= 50ms.
   * Can only be used with multi-stream projections (`fromStreams`).
   * V1 only - has no effect in v2.
   * @default false
   */
  reorderEvents?: boolean;

  /**
   * Delay in ms before processing an event when `reorderEvents` is enabled. Minimum 50ms.
   * V1 only - has no effect in v2.
   */
  processingLag?: number;

  /**
   * Enable bi-state mode. The projection maintains both individual and shared states,
   * initialized via `$init` and `$initShared` respectively.
   * @default false
   */
  biState?: boolean;
}
