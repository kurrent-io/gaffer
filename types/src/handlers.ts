import type { EventBody, KurrentEvent } from "./events.ts";
import type { State } from "./state.ts";

/**
 * The expected handler type for a key in a regular projection.
 * Built-in `$`-prefixed keys have specific signatures; everything else
 * is `(state, event) => state`.
 */
export type HandlerFor<S extends State, K extends string> = K extends "$init"
	? () => S
	: K extends "$initShared"
		? never
		: K extends "$any"
			? (state: S, event: KurrentEvent) => S | null | void
			: K extends "$created"
				? (state: S, event: KurrentEvent) => void
				: K extends "$deleted"
					? (state: S, event: null, partition: string, isSoftDelete: boolean) => void
					: (state: S, event: KurrentEvent) => S | null | void;

/**
 * BiState counterpart to `HandlerFor`. Each handler receives the
 * readonly `[state, shared]` tuple. `$initShared` is the shared-state
 * initialiser; `$deleted` is `never` (biState + `foreachStream` is
 * not supported by the runtime).
 */
export type BiStateHandlerFor<S extends State, TShared extends State, K extends string> = K extends "$init"
	? () => S
	: K extends "$initShared"
		? () => TShared
		: K extends "$any"
			? (state: readonly [S, TShared], event: KurrentEvent) => readonly [S, TShared] | null | void
			: K extends "$created"
				? (state: readonly [S, TShared], event: KurrentEvent) => void
				: K extends "$deleted"
					? never
					: (state: readonly [S, TShared], event: KurrentEvent) => readonly [S, TShared] | null | void;

/**
 * Type of a regular event-name handler. Use this to type a single
 * handler function that's authored standalone (e.g. shared between
 * projections) and then passed to `.when()` by reference. `TBody`
 * lets the caller narrow the event body shape when known:
 *
 * @example
 * ```ts
 * const onOrderPlaced: Projection.EventHandler<OrderState, { amount: number }> = (s, e) => ({
 *   ...s,
 *   total: s.total + (e.body?.amount ?? 0),
 * });
 *
 * fromAll<OrderState>().when({ $init: () => ({ total: 0 }), OrderPlaced: onOrderPlaced });
 * ```
 */
export type EventHandler<S extends State, TBody extends EventBody = EventBody> = (
	state: S,
	event: KurrentEvent<TBody>,
) => S | null | void;

/**
 * Type of a biState event-name handler. The handler receives the
 * readonly `[state, shared]` tuple and returns either a new tuple,
 * `null`, or `void` (preserves state via mutation). `TBody` narrows
 * the event body shape - see `EventHandler` for the regular flavour.
 */
export type BiStateEventHandler<S extends State, TShared extends State, TBody extends EventBody = EventBody> = (
	state: readonly [S, TShared],
	event: KurrentEvent<TBody>,
) => readonly [S, TShared] | null | void;

/**
 * Standalone bundle of the four built-in lifecycle handlers a regular
 * projection can declare: `$init`, `$any`, `$created`, `$deleted`. Use
 * this to factor lifecycle handlers out of `.when()` for sharing or
 * testing. Per-event handlers (`OrderPlaced`, etc.) are not part of
 * this type - declare them inline in `.when()` or with `EventHandler`.
 *
 * For biState lifecycle handlers (`$initShared`, plus the tuple-typed
 * variants), see `BiStateHandlers`.
 */
export type Handlers<S extends State> = {
	$init?: () => S;
	$any?: (state: S, event: KurrentEvent) => S | null | void;
	$created?: (state: S, event: KurrentEvent) => void;
	$deleted?: (state: S, event: null, partition: string, isSoftDelete: boolean) => void;
};

/**
 * BiState counterpart to `Handlers`. `$initShared` is required - its
 * presence is what activates biState mode at the `.when()` call site,
 * which then has handlers receive the readonly `[state, shared]` tuple.
 */
export type BiStateHandlers<S extends State, TShared extends State> = {
	$init?: () => S;
	$initShared: () => TShared;
	$any?: (state: readonly [S, TShared], event: KurrentEvent) => readonly [S, TShared] | null | void;
	$created?: (state: readonly [S, TShared], event: KurrentEvent) => void;
};
