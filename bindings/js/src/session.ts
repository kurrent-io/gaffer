import { getNativeBindings, ERROR_BUF_SIZE, readErrorBuf } from "./native.js";
import { parseErrorJson } from "./errors.js";
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
	private readonly source: string;

	constructor(source: string, options?: SessionOptions) {
		this.source = source;
		const native = getNativeBindings();
		const optionsJson = options ? JSON.stringify(options) : null;
		const errorBuf = Buffer.alloc(ERROR_BUF_SIZE);
		this.handle = native.sessionCreate(
			source,
			optionsJson,
			errorBuf,
			ERROR_BUF_SIZE,
		);
		if (this.handle === 0) {
			this.checkError(errorBuf);
			throw new Error("Failed to create projection session");
		}
	}

	/** Register a callback for emitted events (emit and linkTo). */
	onEmit(cb: (event: EmittedEvent) => void): void {
		this.ensureNotDisposed();
		const handle = getNativeBindings().onEmit(
			this.handle,
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
		const errorBuf = Buffer.alloc(ERROR_BUF_SIZE);
		const result = native.sessionFeed(
			this.handle,
			eventJson,
			errorBuf,
			ERROR_BUF_SIZE,
		);
		if (result !== 0) {
			this.checkError(errorBuf);
			throw new Error("Unknown projection error");
		}
	}

	/** Get current state for a partition, or null if not seen. */
	getState(partition?: string): string | null {
		this.ensureNotDisposed();
		const errorBuf = Buffer.alloc(ERROR_BUF_SIZE);
		const result = getNativeBindings().sessionGetState(
			this.handle,
			partition ?? null,
			errorBuf,
			ERROR_BUF_SIZE,
		);
		this.checkError(errorBuf);
		return result;
	}

	/** Get current state parsed as JSON. */
	getStateJson<T = unknown>(partition?: string): T | null {
		const state = this.getState(partition);
		return state ? (JSON.parse(state) as T) : null;
	}

	/** Get shared state for biState projections. */
	getSharedState(): string | null {
		this.ensureNotDisposed();
		const errorBuf = Buffer.alloc(ERROR_BUF_SIZE);
		const result = getNativeBindings().sessionGetSharedState(
			this.handle,
			errorBuf,
			ERROR_BUF_SIZE,
		);
		this.checkError(errorBuf);
		return result;
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
		const errorBuf = Buffer.alloc(ERROR_BUF_SIZE);
		const result = getNativeBindings().sessionGetResult(
			this.handle,
			partition ?? null,
			errorBuf,
			ERROR_BUF_SIZE,
		);
		this.checkError(errorBuf);
		return result;
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
		const errorBuf = Buffer.alloc(ERROR_BUF_SIZE);
		const result = getNativeBindings().sessionGetPartitionKey(
			this.handle,
			JSON.stringify(event),
			errorBuf,
			ERROR_BUF_SIZE,
		);
		this.checkError(errorBuf);
		return result;
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

	private checkError(errorBuf: Buffer): void {
		const json = readErrorBuf(errorBuf);
		if (json) {
			throw parseErrorJson(json, this.source);
		}
	}

	private ensureNotDisposed(): void {
		if (this.disposed) {
			throw new Error("Session has been disposed");
		}
	}
}
