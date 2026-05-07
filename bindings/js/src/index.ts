export { ProjectionSession } from "./session.js";
export { knownBugs } from "./knownBugs.js";
export type { KnownBug } from "./knownBugs.js";
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
	Diagnostic,
	SourceRange,
	SourcePosition,
} from "./types.js";
export { DiagnosticSeverity } from "./types.js";
