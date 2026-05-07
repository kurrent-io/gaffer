/// <reference path="../src/projections.d.ts" />

type CountState = { count: number };
type TotalState = { total: number };

// --- Regular handlers ---

// Valid: basic projection with typed state
fromAll<CountState>().when({
	$init: () => ({ count: 0 }),
	OrderPlaced: (state, event) => ({ count: state.count + 1 }),
});

// Valid: handler can return null (replaces state)
fromAll<CountState>().when({
	$init: () => ({ count: 0 }),
	OrderPlaced: (_state, _event) => null,
});

// Valid: handler can return void (preserves state via mutation)
fromAll<CountState>().when({
	$init: () => ({ count: 0 }),
	OrderPlaced: (state, _event) => {
		state.count++;
	},
});

// Valid: $any handler
fromAll<CountState>().when({
	$init: () => ({ count: 0 }),
	$any: (state, _event) => ({ count: state.count + 1 }),
});

// Valid: $created and $deleted with foreachStream
fromCategory<CountState>("order")
	.foreachStream()
	.when({
		$init: () => ({ count: 0 }),
		$created: (_state, _event) => {},
		$deleted: (_state, _event, _partition, _isSoftDelete) => {},
		OrderPlaced: (state, _event) => ({ count: state.count + 1 }),
	});

// Valid: $deleted param types
fromCategory<CountState>("order")
	.foreachStream()
	.when({
		$init: () => ({ count: 0 }),
		$deleted: (state, event, partition, isSoftDelete) => {
			const _s: CountState = state;
			const _e: null = event;
			const _p: string = partition;
			const _d: boolean = isSoftDelete;
		},
	});

// Valid: no handlers at all
fromAll().when({});

// Valid: $init only
fromAll<CountState>().when({
	$init: () => ({ count: 0 }),
});

// --- BiState handlers ---

// Valid: biState with tuple destructuring.
//
// `shared` types as `unknown` inside the handler body because TS
// resolves `TShared` after contextual typing of the literal, not
// before. Casting to TotalState is the workaround real users will
// use; the cast is verified by the structural assignability check
// against the constraint at the call site.
fromAll<CountState>().when({
	$init: () => ({ count: 0 }),
	$initShared: () => ({ total: 0 }),
	OrderPlaced: ([state, shared], _event) => [{ count: state.count + 1 }, { total: (shared as TotalState).total + 1 }],
});

// Valid: biState $any handler
fromAll<CountState>().when({
	$init: () => ({ count: 0 }),
	$initShared: () => ({ total: 0 }),
	$any: ([state, shared], _event) => [{ count: state.count + 1 }, { total: (shared as TotalState).total + 1 }],
});

// Valid: biState handler returning void (preserves state)
fromAll<CountState>().when({
	$init: () => ({ count: 0 }),
	$initShared: () => ({ total: 0 }),
	OrderPlaced: ([_state, _shared], _event) => {},
});

// Valid: biState $created (return ignored)
fromAll<CountState>()
	.foreachStream()
	.when({
		$init: () => ({ count: 0 }),
		$initShared: () => ({ total: 0 }),
		$created: ([_state, _shared], _event) => {},
	});

// Valid: without $initShared, falls to regular overload (not biState)
fromAll<CountState>().when({
	$init: () => ({ count: 0 }),
	OrderPlaced: (state, _event) => ({ count: state.count + 1 }),
});

// Valid: biState transformBy receives the [state, shared] tuple, not S.
// Pins the contract that the biState `when()` returns a chain whose
// fold state is the tuple - the runtime passes the array straight in.
const _bistateChain = fromAll<CountState>().when({
	$init: () => ({ count: 0 }),
	$initShared: () => ({ total: 0 }),
	OrderPlaced: ([_state, _shared]: readonly [CountState, TotalState], _event) => {},
});
// `s[0]` and `s[1]` infer as `CountState` and `TotalState` directly -
// no cast needed. Pins the precision of the biState fold-state typing.
_bistateChain.transformBy((s) => {
	const _s0: CountState = s[0];
	const _s1: TotalState = s[1];
	return { count: s[0].count, total: s[1].total };
});

// @ts-expect-error biState transformBy parameter is the tuple, not S; treating it as CountState fails
_bistateChain.transformBy((s: CountState) => ({ count: s.count }));

// biState fold state is a 2-tuple, not an arbitrary array. Indices
// past 1 don't type-check.
// @ts-expect-error tuple length is 2; index 2 is out of bounds
_bistateChain.transformBy((s) => ({ third: s[2] }));

// --- Validation via generic constraint ---

// Disambiguation: a literal containing $initShared MUST land on the
// BiState overload. The regular overload's per-key constraint resolves
// `$initShared` to `never` (HandlerFor<S, "$initShared"> = never), so
// the regular path is unreachable for this shape. If a future change
// weakens that `never`, `transformBy` would silently start receiving
// `S` instead of the tuple - this test pins the disambiguation.
const _disambiguatedAsBiState = fromAll<CountState>().when({
	$init: () => ({ count: 0 }),
	$initShared: () => ({ total: 0 }),
	OrderPlaced: ([_s, _sh]: readonly [CountState, TotalState], _event) => {},
});
_disambiguatedAsBiState.transformBy((s) => {
	// If this assignment fails the disambiguation has regressed.
	const _tuple: readonly [CountState, TotalState] = s;
	return { ok: true };
});

// @ts-expect-error biState handler can't return plain object (must return tuple)
fromAll<CountState>().when({
	$init: () => ({ count: 0 }),
	$initShared: () => ({ total: 0 }),
	OrderPlaced: (state: any, _event: any) => ({ count: state.count + 1 }),
});

// prettier-ignore
// @ts-expect-error biState + $deleted is not allowed (resolves to never)
fromAll<CountState>().foreachStream().when({ $init: () => ({ count: 0 }), $initShared: () => ({ total: 0 }), $deleted: (_s: any, _e: any, _p: any, _d: any) => {} });

// --- Standalone Projection.Handlers usage ---

// Valid: Handlers<S> as a standalone-declared bundle of $-built-ins.
// strict mode + the no-index-signature shape gives precise contextual
// typing on the handler params (no implicit-any).
const _lifecycle: Projection.Handlers<CountState> = {
	$init: () => ({ count: 0 }),
	$any: (state, _event) => ({ count: state.count + 1 }),
	$created: (_state, _event) => {},
	$deleted: (_state, _event, _partition, _isSoftDelete) => {},
};

// Per-event keys aren't part of Handlers<S>; declare them inline in
// `when({...})` or use `Projection.EventHandler<S>` instead.
const _bad: Projection.Handlers<CountState> = {
	$init: () => ({ count: 0 }),
	// @ts-expect-error per-event keys aren't part of Handlers<S>
	OrderPlaced: (state: CountState, _event: Projection.KurrentEvent) => ({ count: state.count + 1 }),
};

// Valid: EventHandler<S> for sharing a single event handler across
// projections. The shared function passes through `.when()` without
// extra annotation.
const onOrderPlaced: Projection.EventHandler<CountState> = (state, _event) => ({
	count: state.count + 1,
});
fromAll<CountState>().when({ $init: () => ({ count: 0 }), OrderPlaced: onOrderPlaced });
