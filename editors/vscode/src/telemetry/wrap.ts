// Catch-emit-rethrow wrappers around the extension's outermost
// surfaces (activate body, command handlers). Production code stays
// `throw err;` shaped - the wrapper observes the exception, hands a
// scrubbed payload to the telemetry sink, then re-throws so the
// extension host's existing error handling (VS Code's "Extension
// X crashed" toast, debug-tracker dispatch, etc.) is unchanged.
//
// The wrappers take a getTelemetry callback rather than a direct
// Telemetry handle so they compose with the activation timeline:
// command-handler wrappers are constructed during activate(),
// before activeTelemetry has been assigned. The callback resolves
// the live handle each time it's invoked.

import type { Phase } from "./exception.js";
import { buildException } from "./exception.js";
import type { Telemetry } from "./facade.js";

export interface WrapContext {
	getTelemetry: () => Telemetry | null;
	extensionPath: string;
	/** Resolved at report time so mid-session workspace folder changes
	 * are honoured. Snapshotting at activation drops user-code frames
	 * from folders open then but not now (and vice versa). */
	getWorkspaceFolders: () => readonly string[];
	log: (line: string) => void;
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
			reportException(ctx, phase, err);
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
			reportException(ctx, phase, err);
			throw err;
		}
	};
}

/**
 * Build + emit an exception envelope for `err`. Public so the
 * activate-body try/catch can call it directly without going
 * through the wrap helpers (those work by wrapping a function;
 * activate is too tangled to wrap as a unit).
 *
 * The report path must never replace the caller's original error -
 * a throw inside emit is suppressed and logged, the caller's catch
 * still sees the original `err` to re-throw.
 */
export function reportException(
	ctx: WrapContext,
	phase: Phase,
	err: unknown,
): void {
	const telemetry = ctx.getTelemetry();
	if (telemetry === null) return;
	try {
		telemetry.emit(
			buildException({
				err,
				phase,
				extensionPath: ctx.extensionPath,
				workspaceFolders: ctx.getWorkspaceFolders(),
			}),
		);
	} catch (reportErr) {
		ctx.log(
			`telemetry: exception report failed: ${reportErr instanceof Error ? reportErr.message : String(reportErr)}`,
		);
	}
}
