import type { KurrentEvent } from "./events.ts";
import type { State } from "./state.ts";

interface BuiltInHandlers<S extends State = State> {
  /** Initializes the projection state. */
  $init?: () => S;

  /** Initializes the shared state object when bistate is enabled. */
  $initShared?: () => S;

  /** Handler for events with any event type. */
  $any?: (state: S, event: KurrentEvent) => S;

  /** Handler called when a partition is created. Can only be used with foreachStream. */
  $created?: (state: S, event: KurrentEvent) => S;

  /** Handler called for each deleted stream. Can only be used with foreachStream. */
  $deleted?: (
    state: S,
    event: null,
    partition: string,
    isSoftDelete: boolean
  ) => S;
}

type EventHandlers<S extends State = State> = {
  [eventType: string]: ((state: S, event: KurrentEvent) => S) | undefined;
};

export type Handlers<S extends State = State> = BuiltInHandlers<S> &
  EventHandlers<S>;
