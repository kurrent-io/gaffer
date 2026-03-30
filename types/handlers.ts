import type { KurrentEvent } from "./events.ts";
import type { State } from "./state.ts";

// --- Regular (non-biState) handlers ---

interface BuiltInHandlers<S extends State = State> {
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
}

type EventHandlers<S extends State = State> = {
  [K in string as K extends keyof BuiltInHandlers | "$initShared" ? never : K]?:
    (state: S, event: KurrentEvent) => S | null | void;
};

export type Handlers<S extends State = State> = BuiltInHandlers<S> &
  EventHandlers<S>;

// --- BiState handlers ---
// When $initShared is present, state is passed as a [state, shared] tuple.
// Handlers receive the tuple and must return a tuple. $deleted is not allowed.

interface BuiltInBiStateHandlers<S extends State, TShared extends State> {
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
}

type BiStateEventHandlers<S extends State, TShared extends State> = {
  [K in string as K extends keyof BuiltInBiStateHandlers<S, TShared> ? never : K]?:
    (state: [S, TShared], event: KurrentEvent) => [S, TShared] | null | void;
};

export type BiStateHandlers<
  S extends State = State,
  TShared extends State = State,
> = BuiltInBiStateHandlers<S, TShared> & BiStateEventHandlers<S, TShared>;
