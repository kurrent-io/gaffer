// Helpers for ordering test assertions around microtask/timer scheduling.
//
// `flushMicrotasks` resolves "after the current microtask queue drains" -
// useful when production code awaits a Promise.resolve() chain before
// observable state catches up. `await flushMicrotasks()` is enough to
// step past 1-2 levels of awaiting; loop a few times for deeper chains.

export const flushMicrotasks = (): Promise<void> =>
	new Promise<void>((resolve) => {
		queueMicrotask(resolve);
	});

// Drain N microtask cycles. Use sparingly - if you need this it usually
// means the production code's async ordering is hard to reason about.
export async function flushAllMicrotasks(cycles = 8): Promise<void> {
	for (let i = 0; i < cycles; i++) await flushMicrotasks();
}
