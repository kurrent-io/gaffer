// Catch-emit-rethrow wrappers around the extension's outermost
// surfaces (activate body, command handlers, view providers).
// Production code stays `throw err;` shaped - the wrapper observes
// the exception, hands a scrubbed payload to the telemetry facade,
// then re-throws so the extension host's existing error handling
// (VS Code's "Extension X crashed" toast, debug-tracker dispatch,
// etc.) is unchanged.

import type { Phase } from "./exception.js";
import type { Telemetry } from "./facade.js";

export interface WrapContext {
	telemetry: Telemetry;
}

/**
 * Wrap an async function so a thrown error fires an `exception`
 * envelope before propagating.
 */
export function wrapAsync<A extends unknown[], R>(
	ctx: WrapContext,
	phase: Phase,
	fn: (...args: A) => PromiseLike<R> | R,
): (...args: A) => Promise<R> {
	return async (...args: A): Promise<R> => {
		try {
			return await fn(...args);
		} catch (err) {
			ctx.telemetry.reportException(phase, err);
			throw err;
		}
	};
}

/**
 * Synchronous variant. Same shape as wrapAsync but for handlers
 * that don't return a promise.
 */
export function wrapSync<A extends unknown[], R>(
	ctx: WrapContext,
	phase: Phase,
	fn: (...args: A) => R,
): (...args: A) => R {
	return (...args: A): R => {
		try {
			return fn(...args);
		} catch (err) {
			ctx.telemetry.reportException(phase, err);
			throw err;
		}
	};
}

/**
 * Direct exception report. Used by the activate-body try/catch that
 * can't be expressed as a function wrap.
 */
export function reportException(
	ctx: WrapContext,
	phase: Phase,
	err: unknown,
): void {
	ctx.telemetry.reportException(phase, err);
}
