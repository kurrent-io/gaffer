import { getNativeBindings } from "./native.js";
import type { IKoffiRegisteredCallback } from "koffi";
import type {
	EmittedEvent,
	ProjectionEvent,
	QuerySources,
	SessionOptions,
} from "./types.js";

/**
 * A projection runtime session. Wraps the native gaffer runtime via FFI.
 * Not thread-safe - do not use from multiple workers concurrently.
 */
export class ProjectionSession {
	private handle: number;
	private disposed = false;
	private registeredCallbacks: IKoffiRegisteredCallback[] = [];

	constructor(source: string, options?: SessionOptions) {
		const native = getNativeBindings();
		const optionsJson = options ? JSON.stringify(options) : null;
		this.handle = native.sessionCreate(source, optionsJson);
		if (this.handle === 0) {
			throw new Error("Failed to create projection session (check JS syntax)");
		}
	}

	/** Register a callback for emitted events (emit and linkTo). */
	onEmit(cb: (event: EmittedEvent) => void): void {
		this.ensureNotDisposed();
		const handle = getNativeBindings().onEmit(
			this.handle,
			(stream, type, data, metadataJson) => {
				const metadata = metadataJson
					? (JSON.parse(metadataJson) as Record<string, string | null>)
					: null;
				cb({
					streamId: stream,
					eventType: type,
					data,
					isJson: type !== "$>" && type !== "$@",
					metadata,
				});
			},
		);
		this.registeredCallbacks.push(handle);
	}

	/** Register a callback for console.log output. */
	onLog(cb: (message: string) => void): void {
		this.ensureNotDisposed();
		const handle = getNativeBindings().onLog(this.handle, cb);
		this.registeredCallbacks.push(handle);
	}

	/** Register a callback for slow handler warnings. */
	onSlowHandler(cb: (handlerName: string, durationMs: number) => void): void {
		this.ensureNotDisposed();
		const handle = getNativeBindings().onSlowHandler(this.handle, cb);
		this.registeredCallbacks.push(handle);
	}

	/** Register a callback for state changes. */
	onStateChanged(
		cb: (partition: string, stateJson: string | null) => void,
	): void {
		this.ensureNotDisposed();
		const handle = getNativeBindings().onStateChanged(this.handle, cb);
		this.registeredCallbacks.push(handle);
	}

	/** Feed a single event to the projection. */
	feed(event: ProjectionEvent): void {
		this.ensureNotDisposed();
		const native = getNativeBindings();
		const eventJson = JSON.stringify(event);
		const result = native.sessionFeed(this.handle, eventJson);
		if (result !== 0) {
			const error = native.sessionGetError(this.handle);
			throw new Error(error ?? "Unknown projection error");
		}
	}

	/** Get current state for a partition, or null if not seen. */
	getState(partition?: string): string | null {
		this.ensureNotDisposed();
		return getNativeBindings().sessionGetState(this.handle, partition ?? null);
	}

	/** Get current state parsed as JSON. */
	getStateJson<T = unknown>(partition?: string): T | null {
		const state = this.getState(partition);
		return state ? (JSON.parse(state) as T) : null;
	}

	/** Get shared state for biState projections. */
	getSharedState(): string | null {
		this.ensureNotDisposed();
		return getNativeBindings().sessionGetSharedState(this.handle);
	}

	/** Get shared state parsed as JSON. */
	getSharedStateJson<T = unknown>(): T | null {
		const state = this.getSharedState();
		return state ? (JSON.parse(state) as T) : null;
	}

	/** Restore state for a partition. */
	setState(partition: string | null, stateJson: string): void {
		this.ensureNotDisposed();
		getNativeBindings().sessionSetState(this.handle, partition, stateJson);
	}

	/** Get the transformed result for a partition. */
	getResult(partition?: string): string | null {
		this.ensureNotDisposed();
		return getNativeBindings().sessionGetResult(this.handle, partition ?? null);
	}

	/** Get the transformed result parsed as JSON. */
	getResultJson<T = unknown>(partition?: string): T | null {
		const result = this.getResult(partition);
		return result ? (JSON.parse(result) as T) : null;
	}

	/** Get the source definition (what the projection reads). */
	getSources(): QuerySources {
		this.ensureNotDisposed();
		const json = getNativeBindings().sessionGetSources(this.handle);
		if (!json) throw new Error("Failed to get sources");
		return JSON.parse(json) as QuerySources;
	}

	/** Get the partition key for an event. */
	getPartitionKey(event: ProjectionEvent): string | null {
		this.ensureNotDisposed();
		return getNativeBindings().sessionGetPartitionKey(
			this.handle,
			JSON.stringify(event),
		);
	}

	/** Release the session and free native resources. */
	dispose(): void {
		if (this.disposed) return;
		this.disposed = true;
		const native = getNativeBindings();
		for (const cb of this.registeredCallbacks) {
			native.unregisterCallback(cb);
		}
		this.registeredCallbacks = [];
		native.sessionDestroy(this.handle);
	}

	/** Implements Symbol.dispose for `using` syntax. */
	[Symbol.dispose](): void {
		this.dispose();
	}

	private ensureNotDisposed(): void {
		if (this.disposed) {
			throw new Error("Session has been disposed");
		}
	}
}
