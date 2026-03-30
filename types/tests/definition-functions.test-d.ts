/// <reference path="../projections.d.ts" />

type CountState = { count: number };

// --- on_event ---

// Valid: basic usage
on_event("OrderPlaced", (state, _event) => state);

// Valid: with typed state
on_event<CountState>("OrderPlaced", (state, _event) => ({ count: state.count + 1 }));

// Valid: returning void (preserves state)
on_event<CountState>("OrderPlaced", (state, _event) => {
  state.count++;
});

// Valid: returning null (replaces state)
on_event<CountState>("OrderPlaced", (_state, _event) => null);

// --- on_any ---

// Valid: basic usage
on_any((state, _event) => state);

// Valid: with typed state
on_any<CountState>((state, _event) => ({ count: state.count + 1 }));

// Valid: returning void
on_any<CountState>((state, _event) => {
  state.count++;
});

// --- Source selector argument validation ---

// @ts-expect-error fromStream requires a string argument
fromStream();

// @ts-expect-error fromStream requires a string argument
fromStream();

// @ts-expect-error options requires an object
options();
