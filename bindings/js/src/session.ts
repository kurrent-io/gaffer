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
 * A projection runtime session. Wraps the native gaffer runtime via FFI.
 * Not thread-safe - do not use from multiple workers concurrently.
 */
export class ProjectionSession {
	#handle: number;
	#disposed = false;
	#registeredCallbacks: IKoffiRegisteredCallback[] = [];
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
	}

	/** Register a callback for emitted events (emit and linkTo). */
	onEmit(cb: (event: EmittedEvent) => void): void {
		this.#ensureNotDisposed();
		const handle = getNativeBindings().onEmit(
			this.#handle,
			(stream, type, data, metadataJson, isJson, isLink) => {
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
			},
		);
		this.#registeredCallbacks.push(handle);
	}

	/** Register a callback for console.log output. */
	onLog(cb: (message: string) => void): void {
		this.#ensureNotDisposed();
		const handle = getNativeBindings().onLog(this.#handle, cb);
		this.#registeredCallbacks.push(handle);
	}

	/** Register a callback for state changes. */
	onStateChanged(
		cb: (partition: string, stateJson: string | null) => void,
	): void {
		this.#ensureNotDisposed();
		const handle = getNativeBindings().onStateChanged(this.#handle, cb);
		this.#registeredCallbacks.push(handle);
	}

	/** Feed a single event to the projection and return the step result. */
	feed(event: ProjectionEvent): FeedResult {
		this.#ensureNotDisposed();
		const { result, errorJson } = getNativeBindings().sessionFeed(
			this.#handle,
			JSON.stringify(event),
		);
		if (errorJson) {
			throw parseErrorJson(errorJson, this.#source);
		}
		if (result == null) {
			throw new Error("Unknown error feeding event");
		}
		return parseFeedResult(result);
	}

	/** Get current state for a partition, or null if not seen. */
	getState(partition?: string): string | null {
		this.#ensureNotDisposed();
		return getNativeBindings().sessionGetState(this.#handle, partition ?? null);
	}

	/** Get current state parsed as JSON. */
	getStateJson<T = unknown>(partition?: string): T | null {
		const state = this.getState(partition);
		return state ? (JSON.parse(state) as T) : null;
	}

	/** Get shared state for biState projections. */
	getSharedState(): string | null {
		this.#ensureNotDisposed();
		return getNativeBindings().sessionGetSharedState(this.#handle);
	}

	/** Get shared state parsed as JSON. */
	getSharedStateJson<T = unknown>(): T | null {
		const state = this.getSharedState();
		return state ? (JSON.parse(state) as T) : null;
	}

	/** Restore state for a partition. */
	setState(partition: string | null, stateJson: string): void {
		this.#ensureNotDisposed();
		getNativeBindings().sessionSetState(this.#handle, partition, stateJson);
	}

	/** Get the transformed result for a partition. */
	getResult(partition?: string): string | null {
		this.#ensureNotDisposed();
		const { result, errorJson } = getNativeBindings().sessionGetResult(
			this.#handle,
			partition ?? null,
		);
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

	/** Get the partition key for an event. */
	getPartitionKey(event: ProjectionEvent): string | null {
		this.#ensureNotDisposed();
		return getNativeBindings().sessionGetPartitionKey(
			this.#handle,
			JSON.stringify(event),
		);
	}

	/** Release the session and free native resources. */
	dispose(): void {
		if (this.#disposed) return;
		this.#disposed = true;
		const native = getNativeBindings();
		for (const cb of this.#registeredCallbacks) {
			native.unregisterCallback(cb);
		}
		this.#registeredCallbacks = [];
		native.sessionDestroy(this.#handle);
	}

	/** Implements Symbol.dispose for `using` syntax. */
	[Symbol.dispose](): void {
		this.dispose();
	}

	#ensureNotDisposed(): void {
		if (this.#disposed) {
			throw new Error("Session has been disposed");
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
