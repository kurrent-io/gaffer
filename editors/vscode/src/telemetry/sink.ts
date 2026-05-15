// Fire-and-forget envelope sink. Each send() POSTs to the ingest
// worker and returns immediately; the call site doesn't await. A
// per-sink Set tracks in-flight requests so drain() can settle them
// on extension deactivate with a bounded grace period.
//
// No buffer / queue: the extension emits at most one extension_activated
// per lifetime plus rare exceptions - rates a queue can't help with.
// No retry: the worker always returns 200 (even on validation drops),
// so a non-2xx response is unrecoverable network failure and adding a
// retry would only delay a doomed envelope.

import type { Envelope } from "@kurrent/gaffer-telemetry";

/**
 * Ingest endpoint. Substituted at build time by Vite (`define`) - dev
 * builds get the staging worker, `vite build --mode production` flips
 * to the prod worker. See vite.config.ts.
 */
declare const __INGEST_URL__: string;
export const INGEST_URL = __INGEST_URL__;

/** Per-request abort after this many ms - matches CLI's net timeout. */
const REQUEST_TIMEOUT_MS = 5000;

export interface SinkOptions {
	url: string;
	/** When true, every envelope is logged as JSON before send. */
	debug: boolean;
	/** Where the debug print goes (typically the gaffer output channel). */
	log: (line: string) => void;
	/** Override `fetch` for tests. Defaults to global `fetch`. */
	fetchImpl?: typeof globalThis.fetch;
}

export interface Sink {
	/** Fire-and-forget POST. Never throws; never blocks the caller. */
	send(envelope: Envelope): void;
	/**
	 * Wait for all in-flight sends to settle, or the timeout to elapse.
	 * Intended for `deactivate()` so the editor doesn't kill the
	 * extension host with sends still in the queue.
	 *
	 * Invariant: callers must stop emitting before calling drain. The
	 * snapshot of pending is taken at drain entry, so a send that lands
	 * mid-drain is not awaited and may be abandoned if the timeout
	 * wins. deactivate is naturally well-behaved here (no further emits
	 * happen after the editor has signalled shutdown); a future caller
	 * that wires drain into a different lifecycle would need to gate
	 * its own emit path.
	 */
	drain(timeoutMs: number): Promise<void>;
}

export function createSink(opts: SinkOptions): Sink {
	const pending = new Set<Promise<unknown>>();
	const fetchImpl = opts.fetchImpl ?? globalThis.fetch;

	const send = (envelope: Envelope): void => {
		if (opts.debug) {
			opts.log(`gaffer-telemetry: ${JSON.stringify(envelope)}`);
		}
		const p = fetchImpl(opts.url, {
			method: "POST",
			headers: { "content-type": "application/json" },
			body: JSON.stringify(envelope),
			signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
		})
			// Swallow everything: network failure, abort, non-2xx. The
			// worker is always 200; anything else is gaffer infra
			// unavailable, which the user can't act on. Logging would be
			// noise.
			.catch(() => undefined)
			.finally(() => {
				pending.delete(p);
			});
		pending.add(p);
	};

	const drain = async (timeoutMs: number): Promise<void> => {
		if (pending.size === 0) return;
		let timer: ReturnType<typeof setTimeout> | undefined;
		const timeout = new Promise<void>((resolve) => {
			timer = setTimeout(resolve, timeoutMs);
		});
		try {
			await Promise.race([
				Promise.allSettled([...pending]).then(() => undefined),
				timeout,
			]);
		} finally {
			// Clear the timer when settle wins the race - leaving it
			// scheduled would keep the extension-host event loop alive
			// past drain's return and delay deactivate.
			if (timer !== undefined) clearTimeout(timer);
		}
	};

	return { send, drain };
}
