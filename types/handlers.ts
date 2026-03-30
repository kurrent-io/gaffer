import type { KurrentEvent } from "./events.ts";
import type { State } from "./state.ts";

interface ProjectionHandlers<S extends State = State> {
  /**
   * Handler for events with any event type.
   * @throws {Error} If handler returns undefined or null.
   */
  $any?: (state: S, event: KurrentEvent<S>) => S;

  /**
   * Initializes the projection state.
   * @throws {Error} If handler returns undefined or null.
   */
  $init?: () => S;

  /**
   * Initializes the shared state object when bistate is enabled.
   * @throws {Error} If handler returns undefined or null.
   * @throws {Error} If used when bistate is not enabled.
   */
  $initShared?: () => S;

  /**
   * Handler called for each deleted stream. Can only be used with `foreachStream`.
   * @throws {Error} If used without foreachStream.
   * @throws {Error} If handler returns undefined or null.
   */
  $deleted?: (
    state: S,
    event: null,
    partition: string,
    isSoftDelete: boolean
  ) => S;
}

export type Handlers<S extends State = State> = ProjectionHandlers<S> & {
  /**
   * Event type specific handlers. Each handler receives the current state and event,
   * and must return a new state.
   *
   * @throws {Error} If any handler returns undefined or null
   * @throws {Error} If any handler throws an unhandled exception
   */
  [eventType: string]: (state: S, event: KurrentEvent<S>) => S;
};
