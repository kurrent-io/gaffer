/// <reference path="../projections.d.ts" />

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
fromCategory<CountState>("order").foreachStream().when({
  $init: () => ({ count: 0 }),
  $created: (_state, _event) => {},
  $deleted: (_state, _event, _partition, _isSoftDelete) => {},
  OrderPlaced: (state, _event) => ({ count: state.count + 1 }),
});

// Valid: $deleted event param is null
fromCategory("order").foreachStream().when({
  $deleted: (_state, event, _partition, _isSoftDelete) => {
    const _check: null = event;
  },
});

// Valid: no handlers at all
fromAll().when({});

// Valid: $init only
fromAll<CountState>().when({
  $init: () => ({ count: 0 }),
});

// --- BiState handlers ---

// Valid: biState with tuple destructuring
fromAll<CountState>().when({
  $init: () => ({ count: 0 }),
  $initShared: () => ({ total: 0 }),
  OrderPlaced: ([state, shared], _event) => [
    { count: state.count + 1 },
    { total: shared.total + 1 },
  ],
});

// Valid: biState $any handler
fromAll<CountState>().when({
  $init: () => ({ count: 0 }),
  $initShared: () => ({ total: 0 }),
  $any: ([state, shared], _event) => [
    { count: state.count + 1 },
    { total: shared.total + 1 },
  ],
});

// Valid: biState handler returning void (preserves state)
fromAll<CountState>().when({
  $init: () => ({ count: 0 }),
  $initShared: () => ({ total: 0 }),
  OrderPlaced: ([_state, _shared], _event) => {},
});

// Valid: biState $created (return ignored)
fromAll<CountState>().foreachStream().when({
  $init: () => ({ count: 0 }),
  $initShared: () => ({ total: 0 }),
  $created: ([_state, _shared], _event) => {},
});
