export { ProjectionSession } from "./session.js";
export {
	ProjectionError,
	InvalidProjectionError,
	CompilationTimeoutError,
	InvalidArgumentError,
	ProjectionHandlerError,
	ExecutionTimeoutError,
	MalformedEventError,
	StateSerializationError,
	ProjectionTransformError,
} from "./errors.js";
export type { EventContext } from "./errors.js";
export type {
	ProjectionEvent,
	EmittedEvent,
	FeedResult,
	ProjectionInfo,
	SessionOptions,
} from "./types.js";
