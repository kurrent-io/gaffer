export { createProjection } from "./createProjection.js";
export { systemEvents } from "./systemEvents.js";
export { ProjectionTest } from "./ProjectionTest.js";
export { mapQuerySources } from "./ProjectionInfo.js";

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
} from "@kurrent/gaffer-runtime";

export type { Projection, ValidationResult } from "./createProjection.js";
export type { StepResult, TestEmittedEvent } from "./ProjectionTest.js";
export type { ProjectionInfo } from "./ProjectionInfo.js";
export type { TestEvent, EventInput, NormalizedEvent } from "./schemas.js";
export type { EventContext } from "@kurrent/gaffer-runtime";
