// Discriminated unions for the gaffer CLI's JSON output (one object per stdout line)
// and the DAP custom events the extension consumes from the debug session.

export interface ProjectionMetadata {
	name: string;
	source?: string;
	partitioning?: string;
	events?: string[];
	engineVersion?: number;
}

export interface InputEvent {
	sequenceNumber: number;
	streamId: string;
	eventType: string;
	data?: unknown;
	metadata?: unknown;
}

export interface EmittedEvent {
	streamId: string;
	eventType?: string;
	data?: unknown;
	metadata?: unknown;
	isLink?: boolean;
}

export type ProcessedResult = {
	status: "processed";
	partition?: string;
	state?: unknown;
	result?: unknown;
	logs?: string[];
	emitted?: EmittedEvent[];
};

export type SkippedResult = {
	status: "skipped";
	reason: string;
};

export type StepResult = ProcessedResult | SkippedResult;

export interface SummaryStats {
	handled: number;
	skipped: number;
	errors: number;
}

export type CliMessage =
	| { type: "info"; projection: ProjectionMetadata }
	| ({ type: "event" } & InputEvent)
	| ({ type: "result" } & StepResult)
	| { type: "error"; code: string; description: string }
	| ({ type: "summary" } & SummaryStats)
	| { type: "debug"; port: number }
	// Synthesized by GafferSession on child-process exit; not emitted by the CLI itself.
	| { type: "exit"; code: number | null };

export type CliMessageType = CliMessage["type"];

// DAP custom events emitted by the gaffer CLI's DAP server.
export interface StepStartBody {
	event: InputEvent;
}
export interface StepLogBody {
	message: string;
}
export type StepEmitBody = EmittedEvent;
export interface StepResultBody {
	result: StepResult;
}
export interface StepErrorBody {
	code: string;
	description: string;
}
export interface StateBody {
	state?: unknown;
	result?: unknown;
	sharedState?: unknown;
	partitions?: string[];
}
export interface ModeBody {
	mode: "inspect" | "running" | string;
}

// DAP custom request response for gaffer/partitionState.
export interface PartitionStateResponse {
	state?: unknown;
	result?: unknown;
}

// CLI manifest returned by `gaffer manifest`.
export interface Manifest {
	version: string;
	commands: Record<string, { flags?: string[] }>;
}

// Tracked across the extension to drive code lens UI.
export interface DebugState {
	name: string | null;
	status: "idle" | "starting" | "debugging";
}

// Resolved entry from ProjectIndex.
export interface ProjectEntry {
	name: string;
	tomlDir: string;
}
