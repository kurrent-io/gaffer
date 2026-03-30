import type { KurrentEvent } from "./events.ts";
import type { State } from "./state.ts";

// The index signature is a union: the typed handler signature (for contextual
// typing of custom event handlers) plus a loose fallback (so $deleted's
// 4-param signature doesn't conflict). TypeScript contextually types handler
// params from the first union member, giving proper IntelliSense.

export type Handlers<S extends State = State> = {
  /** Initializes the projection state. */
  $init?: () => S;

  /** Handler for events with any event type. */
  $any?: (state: S, event: KurrentEvent) => S | null | void;

  /**
   * Handler called when a partition is created. Can only be used with foreachStream.
   * Return value is ignored - only side effects (emit, linkTo) are preserved.
   */
  $created?: (state: S, event: KurrentEvent) => void;

  /**
   * Handler called for each deleted stream. Can only be used with foreachStream.
   * Return value is ignored - only in-place state mutations are preserved.
   */
  $deleted?: (
    state: S,
    event: null,
    partition: string,
    isSoftDelete: boolean
  ) => void;

  /** Event type specific handlers. */
  [eventType: string]:
    | ((state: S, event: KurrentEvent) => S | null | void)
    | ((...args: any[]) => any)
    | undefined;
};

// --- BiState handlers ---
// When $initShared is present, state is passed as a [state, shared] tuple.
// Handlers receive the tuple and must return a tuple. $deleted is not allowed.

export type BiStateHandlers<
  S extends State = State,
  TShared extends State = State,
> = {
  /** Initializes the individual partition state. */
  $init?: () => S;

  /** Initializes the shared state. Presence of this handler enables biState mode. */
  $initShared: () => TShared;

  /** Handler for events with any event type. */
  $any?: (
    state: [S, TShared],
    event: KurrentEvent
  ) => [S, TShared] | null | void;

  /**
   * Handler called when a partition is created. Can only be used with foreachStream.
   * Return value is ignored.
   */
  $created?: (state: [S, TShared], event: KurrentEvent) => void;

  /** Event type specific handlers. */
  [eventType: string]:
    | ((state: [S, TShared], event: KurrentEvent) => [S, TShared] | null | void)
    | ((...args: any[]) => any)
    | undefined;
};
