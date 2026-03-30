import type { KurrentEvent } from "./events.ts";
import type { State } from "./state.ts";

interface BuiltInHandlers<S extends State = State> {
  /** Initializes the projection state. */
  $init?: () => S;

  /** Initializes the shared state object when bistate is enabled. */
  $initShared?: () => S;

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
  [K in string as K extends keyof BuiltInHandlers ? never : K]?:
    (state: S, event: KurrentEvent) => S | null | void;
};

export type Handlers<S extends State = State> = BuiltInHandlers<S> &
  EventHandlers<S>;
