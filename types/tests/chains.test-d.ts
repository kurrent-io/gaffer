/// <reference path="../src/projections.d.ts" />

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

// --- Invalid chain transitions ---

// @ts-expect-error when -> when (double when)
fromAll().when({}).when({});

// prettier-ignore
// @ts-expect-error when -> partitionBy (not available after when)
fromAll().when({}).partitionBy((_e) => "key");

// @ts-expect-error when -> foreachStream (not available after when)
fromAll().when({}).foreachStream();

// prettier-ignore
// @ts-expect-error partitionBy -> outputTo (only when is available)
fromAll().partitionBy((_e) => "key").outputTo("x");

// prettier-ignore
// @ts-expect-error partitionBy -> outputState (only when is available)
fromAll().partitionBy((_e) => "key").outputState();

// @ts-expect-error foreachStream -> outputTo (only when is available)
fromAll().foreachStream().outputTo("x");

// @ts-expect-error foreachStream -> foreachStream (only when is available)
fromAll().foreachStream().foreachStream();

// prettier-ignore
// @ts-expect-error foreachStream -> partitionBy (only when is available)
fromAll().foreachStream().partitionBy((_e) => "key");

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
fromAll().when({}).outputState();

// Valid: outputState -> transformBy -> outputTo
fromAll<OrderState>()
	.when({ $init: () => ({ orders: [], count: 0 }) })
	.outputState()
	.transformBy((s) => ({ doubled: s.count * 2 }))
	.outputTo("results");

// --- Transform generic propagation ---

// Valid: chained transforms - shape changes through each step
fromAll<OrderState>()
	.when({ $init: () => ({ orders: [], count: 0 }) })
	.transformBy((s) => ({ count: s.orders.length }))
	.transformBy((s) => ({ label: `${s.count} orders` }))
	.outputTo("results");

// transformBy changes the type - old properties no longer available
fromAll<OrderState>()
	.when({ $init: () => ({ orders: [], count: 0 }) })
	.transformBy((s) => ({ total: s.count }))
	.filterBy((s) => {
		const _check: number = s.total;
		return s.total > 0;
	})
	.outputTo("results");

// filterBy preserves the type - subsequent transformBy still sees original shape
fromAll<OrderState>()
	.when({ $init: () => ({ orders: [], count: 0 }) })
	.filterBy((s) => s.count > 0)
	.transformBy((s) => {
		const _check: string[] = s.orders;
		return { summary: s.count };
	})
	.outputTo("results");

// Valid: transformBy -> outputState -> transformBy -> outputTo
fromAll<OrderState>()
	.when({ $init: () => ({ orders: [], count: 0 }) })
	.transformBy((s) => ({ total: s.count }))
	.outputState()
	.transformBy((s) => ({ doubled: s.total * 2 }))
	.outputTo("results");

// --- partitionBy ---

// Valid: partitionBy returns string, number, null, or undefined
fromAll()
	.partitionBy((_e) => "key")
	.when({});
fromAll()
	.partitionBy((_e) => 42)
	.when({});
fromAll()
	.partitionBy((_e) => null)
	.when({});
fromAll()
	.partitionBy((_e) => undefined)
	.when({});

// --- outputTo ---

// @ts-expect-error outputTo requires at least one string argument
fromAll().when({}).outputTo();

// @ts-expect-error outputTo first arg must be string
fromAll().when({}).outputTo(42);
