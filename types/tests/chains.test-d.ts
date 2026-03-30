/// <reference path="../projections.d.ts" />

type OrderState = { orders: string[]; count: number };

// --- Source selection ---

// Valid: all source selectors
fromStream("my-stream").when({});
fromAll().when({});
fromCategory("order").when({});
fromCategories(["order", "cart"]).when({});
fromCategories("order", "cart").when({});
fromStreams(["stream-1", "stream-2"]).when({});
fromStreams("stream-1", "stream-2").when({});

// Valid: fromCategory accepts array and variadic (same as fromCategories)
fromCategory(["order", "cart"]).when({});
fromCategory("order", "cart", "user").when({});

// --- Chain availability ---

// Valid: fromAll has foreachStream
fromAll().foreachStream().when({});

// Valid: fromCategory has foreachStream
fromCategory("order").foreachStream().when({});

// @ts-expect-error fromStream does NOT have foreachStream
fromStream("my-stream").foreachStream();

// @ts-expect-error fromStreams does NOT have foreachStream
fromStreams(["a", "b"]).foreachStream();

// --- Transform chains ---

// Valid: when -> transformBy -> outputTo
fromAll<OrderState>()
  .when({ $init: () => ({ orders: [], count: 0 }) })
  .transformBy((s) => ({ summary: s.count }))
  .outputTo("results");

// Valid: when -> filterBy -> outputTo
fromAll<OrderState>()
  .when({ $init: () => ({ orders: [], count: 0 }) })
  .filterBy((s) => s.count > 0)
  .outputTo("results");

// Valid: when -> outputState
fromAll()
  .when({})
  .outputState();

// Valid: outputState -> transformBy -> outputTo
fromAll<OrderState>()
  .when({ $init: () => ({ orders: [], count: 0 }) })
  .outputState()
  .transformBy((s) => ({ doubled: s.count * 2 }))
  .outputTo("results");

// Valid: chained transforms (shape changes through each step)
fromAll<OrderState>()
  .when({ $init: () => ({ orders: [], count: 0 }) })
  .transformBy((s) => ({ count: s.orders.length }))
  .transformBy((s) => ({ label: `${s.count} orders` }))
  .outputTo("results");

// @ts-expect-error partitionBy -> when only (no direct outputTo)
fromAll().partitionBy((_e) => "key").outputTo("x");

// @ts-expect-error foreachStream -> when only (no direct outputTo)
fromAll().foreachStream().outputTo("x");

// Valid: partitionBy returns string, number, null, or undefined
fromAll().partitionBy((_e) => "key").when({});
fromAll().partitionBy((_e) => 42).when({});
fromAll().partitionBy((_e) => null).when({});
fromAll().partitionBy((_e) => undefined).when({});
