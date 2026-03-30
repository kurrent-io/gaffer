import type { KurrentEvent } from "./events.ts";
import type { State } from "./state.ts";

// Maps each handler key to its expected type. Built-in $ handlers get their
// specific signatures; everything else gets the standard event handler type.
// This avoids index signatures entirely, so $deleted's 4-param signature
// doesn't conflict with the 2-param event handler type.

/** Resolves the expected handler type for a given key. */
export type HandlerFor<S extends State, K extends string> =
  K extends "$init" ? () => S :
  K extends "$initShared" ? never :
  K extends "$any" ? (state: S, event: KurrentEvent) => S | null | void :
  K extends "$created" ? (state: S, event: KurrentEvent) => void :
  K extends "$deleted" ? (state: S, event: null, partition: string, isSoftDelete: boolean) => void :
  (state: S, event: KurrentEvent) => S | null | void;

/** Resolves the expected biState handler type for a given key. */
export type BiStateHandlerFor<S extends State, TShared extends State, K extends string> =
  K extends "$init" ? () => S :
  K extends "$initShared" ? () => TShared :
  K extends "$any" ? (state: [S, TShared], event: KurrentEvent) => [S, TShared] | null | void :
  K extends "$created" ? (state: [S, TShared], event: KurrentEvent) => void :
  K extends "$deleted" ? never :
  (state: [S, TShared], event: KurrentEvent) => [S, TShared] | null | void;

/**
 * Convenience type for referencing handlers outside of `when()`.
 * The real validation happens via the self-referential generic on WhenFn.
 */
export type Handlers<S extends State = State> = {
  $init?: () => S;
  $any?: (state: S, event: KurrentEvent) => S | null | void;
  $created?: (state: S, event: KurrentEvent) => void;
  $deleted?: (state: S, event: null, partition: string, isSoftDelete: boolean) => void;
  [eventType: string]:
    | ((state: S, event: KurrentEvent) => S | null | void)
    | ((...args: any[]) => any)
    | undefined;
};

export type BiStateHandlers<
  S extends State = State,
  TShared extends State = State,
> = {
  $init?: () => S;
  $initShared: () => TShared;
  $any?: (state: [S, TShared], event: KurrentEvent) => [S, TShared] | null | void;
  $created?: (state: [S, TShared], event: KurrentEvent) => void;
  [eventType: string]:
    | ((state: [S, TShared], event: KurrentEvent) => [S, TShared] | null | void)
    | ((...args: any[]) => any)
    | undefined;
};
