/**
 * The shape of the projection's fold state. Two flavours:
 *   - `Record<string, unknown>` for regular projections.
 *   - {@link BiStateFold} `readonly [S, TShared]` for biState
 *     projections.
 */
export type State = Record<string, unknown> | BiStateFold;

/**
 * The fold-state shape for biState projections: a 2-tuple of
 * `[partitionState, sharedState]`.
 */
export type BiStateFold = readonly [unknown, unknown];
