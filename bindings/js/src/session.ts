import { getNativeBindings } from "./native.js";
import { parseErrorJson } from "./errors.js";
import type { IKoffiRegisteredCallback } from "koffi";
import type {
	Diagnostic,
	EmittedEvent,
	FeedResult,
	ProjectionEvent,
	ProjectionInfo,
	SessionOptions,
} from "./types.js";

/**
 * Native resources backing a session, kept in a standalone object so the
 * FinalizationRegistry can release them without referencing (and thus pinning)
 * the ProjectionSession itself. Shared by dispose() and the finalizer, so the
 * `released` flag prevents a double free across the two paths.
 */
interface SessionResources {
	handle: number;
	callbacks: IKoffiRegisteredCallback[];
	released: boolean;
	// First exception thrown by a registered callback during the current native
	// call, boxed so a thrown `undefined` is still distinguishable from "none".
	// koffi swallows callback throws at the FFI boundary, so we stash here and
	// rethrow once the native call returns (see #rethrowCallbackError). It lives
	// on resources, not the session, so the koffi callback closures can reach it
	// without capturing `this` - which would pin the session and defeat the
	// FinalizationRegistry below.
	callbackError: { value: unknown } | undefined;
}

// Keep the first error; later callbacks during the same native call don't
// clobber it. A free function so the callback closures stash via the resources
// object rather than the session.
function stashCallbackError(resources: SessionResources, error: unknown): void {
	resources.callbackError ??= { value: error };
}

function releaseResources(resources: SessionResources): void {
	if (resources.released) return;
	resources.released = true;
	const native = getNativeBindings();
	for (const cb of resources.callbacks) {
		// Best-effort: a failing unregister must not skip sessionDestroy below.
		try {
			native.unregisterCallback(cb);
		} catch {
			// ignore - releasing the native session is what matters
		}
	}
	resources.callbacks = [];
	native.sessionDestroy(resources.handle);
}

/**
 * Releases the native session and koffi callback slots for any session that is
 * garbage-collected without dispose(). koffi has a hard 8192-callback-slot cap,
 * so a long-running process that leaks sessions would otherwise eventually fail
 * every future callback registration. This is a safety net - dispose() (or a
 * `using` declaration) is still the correct way to release a session promptly.
 *
 * The body is guarded: an exception thrown from a FinalizationRegistry callback
 * is swallowed by the host and can't be acted on, so suppressing it keeps a bad
 * unregister from surfacing as an unhandled rejection.
 */
const sessionFinalization = new FinalizationRegistry<SessionResources>(
	(resources) => {
		try {
			releaseResources(resources);
		} catch {
			// nothing actionable from inside a finalizer
		}
	},
);

/**
 * A projection runtime session. Wraps the native gaffer runtime via FFI.
 * Not thread-safe - do not use from multiple workers concurrently.
 */
export class ProjectionSession {
	#handle: number;
	#resources: SessionResources;
	readonly #source: string;

	constructor(source: string, options: SessionOptions) {
		this.#source = source;
		const native = getNativeBindings();
		const optionsJson = JSON.stringify(options);
		const { handle, errorJson } = native.sessionCreate(source, optionsJson);
		if (errorJson) {
			throw parseErrorJson(errorJson, source);
		}
		if (handle === 0) {
			throw new Error("Unknown error creating session");
		}
		this.#handle = handle;
		this.#resources = {
			handle,
			callbacks: [],
			released: false,
			callbackError: undefined,
		};
		sessionFinalization.register(this, this.#resources, this.#resources);
	}

	/** Register a callback for emitted events (emit and linkTo). */
	onEmit(cb: (event: EmittedEvent) => void): void {
		this.#ensureNotDisposed();
		// Capture resources, not `this` - the koffi-retained closure must not
		// reference the session or it pins it past GC (see SessionResources).
		const resources = this.#resources;
		const handle = getNativeBindings().onEmit(
			this.#handle,
			(stream, type, data, metadataJson, isJson, isLink) => {
				try {
					const metadata = metadataJson
						? (JSON.parse(metadataJson) as Record<string, string | null>)
						: null;
					cb({
						streamId: stream,
						eventType: type,
						data,
						isJson,
						isLink,
						metadata,
					});
				} catch (error) {
					stashCallbackError(resources, error);
				}
			},
		);
		resources.callbacks.push(handle);
	}

	/** Register a callback for console.log output. */
	onLog(cb: (message: string) => void): void {
		this.#ensureNotDisposed();
		const resources = this.#resources;
		const handle = getNativeBindings().onLog(this.#handle, (message) => {
			try {
				cb(message);
			} catch (error) {
				stashCallbackError(resources, error);
			}
		});
		resources.callbacks.push(handle);
	}

	/** Register a callback for state changes. */
	onStateChanged(
		cb: (partition: string, stateJson: string | null) => void,
	): void {
		this.#ensureNotDisposed();
		const resources = this.#resources;
		const handle = getNativeBindings().onStateChanged(
			this.#handle,
			(partition, state) => {
				try {
					cb(partition, state);
				} catch (error) {
					stashCallbackError(resources, error);
				}
			},
		);
		resources.callbacks.push(handle);
	}

	/** Feed a single event to the projection and return the step result. */
	feed(event: ProjectionEvent): FeedResult {
		this.#ensureNotDisposed();
		const { result, errorJson } = getNativeBindings().sessionFeed(
			this.#handle,
			JSON.stringify(event),
		);
		this.#rethrowCallbackError();
		if (errorJson) {
			throw parseErrorJson(errorJson, this.#source);
		}
		if (result == null) {
			throw new Error("Unknown error feeding event");
		}
		return parseFeedResult(result);
	}

	/** Get current state for a partition, or null if not seen. Throws if the
	 * lookup fails - distinct from a not-seen null. */
	getState(partition?: string): string | null {
		this.#ensureNotDisposed();
		const { result, errorJson } = getNativeBindings().sessionGetState(
			this.#handle,
			partition ?? null,
		);
		if (errorJson) {
			throw parseErrorJson(errorJson, this.#source);
		}
		return result;
	}

	/** Get current state parsed as JSON. */
	getStateJson<T = unknown>(partition?: string): T | null {
		const state = this.getState(partition);
		return state ? (JSON.parse(state) as T) : null;
	}

	/** Get shared state for biState projections. Throws if the lookup fails. */
	getSharedState(): string | null {
		this.#ensureNotDisposed();
		const { result, errorJson } = getNativeBindings().sessionGetSharedState(
			this.#handle,
		);
		if (errorJson) {
			throw parseErrorJson(errorJson, this.#source);
		}
		return result;
	}

	/** Get shared state parsed as JSON. */
	getSharedStateJson<T = unknown>(): T | null {
		const state = this.getSharedState();
		return state ? (JSON.parse(state) as T) : null;
	}

	/** Restore state for a partition. Throws if the runtime rejects the state
	 * (previously a failed restore was silent). */
	setState(partition: string | null, stateJson: string): void {
		this.#ensureNotDisposed();
		const errorJson = getNativeBindings().sessionSetState(
			this.#handle,
			partition,
			stateJson,
		);
		if (errorJson) {
			throw parseErrorJson(errorJson, this.#source);
		}
	}

	/** Get the transformed result for a partition. */
	getResult(partition?: string): string | null {
		this.#ensureNotDisposed();
		const { result, errorJson } = getNativeBindings().sessionGetResult(
			this.#handle,
			partition ?? null,
		);
		this.#rethrowCallbackError();
		if (errorJson) {
			throw parseErrorJson(errorJson, this.#source);
		}
		return result;
	}

	/** Get the transformed result parsed as JSON. */
	getResultJson<T = unknown>(partition?: string): T | null {
		const result = this.getResult(partition);
		return result ? (JSON.parse(result) as T) : null;
	}

	/** Get the source definition (what the projection reads). */
	getSources(): ProjectionInfo {
		this.#ensureNotDisposed();
		const { result, errorJson } = getNativeBindings().sessionGetSources(
			this.#handle,
		);
		if (errorJson) {
			throw parseErrorJson(errorJson, this.#source);
		}
		if (!result) throw new Error("Failed to get sources");
		return JSON.parse(result) as ProjectionInfo;
	}

	/** Get the partition key for an event, or null if unpartitioned. Throws if
	 * the computation fails (e.g. a throwing partitionBy). */
	getPartitionKey(event: ProjectionEvent): string | null {
		this.#ensureNotDisposed();
		const { result, errorJson } = getNativeBindings().sessionGetPartitionKey(
			this.#handle,
			JSON.stringify(event),
		);
		this.#rethrowCallbackError();
		if (errorJson) {
			throw parseErrorJson(errorJson, this.#source);
		}
		return result;
	}

	/** Release the session and free native resources. */
	dispose(): void {
		if (this.#resources.released) return;
		sessionFinalization.unregister(this.#resources);
		releaseResources(this.#resources);
	}

	/** Implements Symbol.dispose for `using` syntax. */
	[Symbol.dispose](): void {
		this.dispose();
	}

	#ensureNotDisposed(): void {
		if (this.#resources.released) {
			throw new Error("Session has been disposed");
		}
	}

	#rethrowCallbackError(): void {
		const stashed = this.#resources.callbackError;
		if (stashed) {
			this.#resources.callbackError = undefined;
			throw stashed.value;
		}
	}
}

function parseFeedResult(json: string): FeedResult {
	const raw = JSON.parse(json) as Record<string, unknown>;
	const result: FeedResult = {
		status: raw.status as FeedResult["status"],
	};

	if (raw.reason != null) result.reason = raw.reason as string;
	if (raw.partition != null) result.partition = raw.partition as string;
	if (raw.state != null) result.state = raw.state;
	if (raw.result != null) result.result = raw.result;
	if (raw.sharedState != null) result.sharedState = raw.sharedState;
	if (raw.emitted != null) result.emitted = raw.emitted as EmittedEvent[];
	if (raw.logs != null) result.logs = raw.logs as string[];
	if (raw.diagnostics != null)
		result.diagnostics = raw.diagnostics as Diagnostic[];

	return result;
}
