export { createProjection } from "./createProjection.js";
export { systemEvents } from "./systemEvents.js";

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

export type { Projection } from "./createProjection.js";
export type {
	DatabaseConfig,
	ProjectionConfig,
	ProjectionOptions,
	StepResult,
	ProcessedStepResult,
	SkippedStepResult,
	SkipReason,
	TestEmittedEvent,
} from "./ProjectionTest.js";
export type { ProjectionTest } from "./ProjectionTest.js";
export type { ProjectionInfo } from "./ProjectionInfo.js";
export type { TestEvent, EventInput } from "./schemas.js";
export type { EventContext } from "@kurrent/gaffer-runtime";
