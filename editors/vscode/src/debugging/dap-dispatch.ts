// Pure routing for DAP custom events emitted by the gaffer CLI's DAP
// server. Each event body is validated against its schema; on parse
// failure the dispatch is skipped and the issue is logged.
//
// Kept separate from extension.ts so the switch can be reasoned about as
// a function of (event, providers) without sharing the activate()
// closure.

import * as vscode from "vscode";
import * as v from "valibot";
import { log } from "../output.js";
import {
	CaughtUpBodySchema,
	FinalStateBodySchema,
	ModeBodySchema,
	StateBodySchema,
	StatsBodySchema,
	StepEmitBodySchema,
	StepErrorBodySchema,
	StepLogBodySchema,
	StepResultBodySchema,
	StepStartBodySchema,
	StepWarningBodySchema,
} from "./schemas.js";
import type { StateProvider } from "../panels/state.js";
import type { StatusViewProvider } from "../panels/status.js";
import type { StepProvider } from "../panels/step.js";
import type { PhaseTracker } from "./phase-tracker.js";

export interface DapHandlers {
	stepProvider: StepProvider;
	stateProvider: StateProvider;
	statusProvider: StatusViewProvider;
	phaseTracker: PhaseTracker;
	setEngineMode: (mode: "running" | "inspecting") => Promise<void> | void;
}

export async function dispatchDapEvent(
	e: vscode.DebugSessionCustomEvent,
	handlers: DapHandlers,
): Promise<void> {
	if (e.session.type !== "gaffer") return;
	handlers.stateProvider.setDebugSession(e.session);
	// Any custom event from the gaffer DAP server proves the CLI is
	// running and talking to us; promote out of the "Connecting"
	// phase. Specific signals (stats / caughtUp) refine the state
	// further below.
	handlers.phaseTracker.noteSignal();

	switch (e.event) {
		case "gaffer/stepStart": {
			const body = parseDapBody(StepStartBodySchema, e);
			if (body) handlers.stepProvider.startStep(body.event);
			break;
		}
		case "gaffer/stepLog": {
			const body = parseDapBody(StepLogBodySchema, e);
			if (body) handlers.stepProvider.addLog(body.message);
			break;
		}
		case "gaffer/stepEmit": {
			const body = parseDapBody(StepEmitBodySchema, e);
			if (body) handlers.stepProvider.addEmit(body);
			break;
		}
		case "gaffer/stepResult": {
			const body = parseDapBody(StepResultBodySchema, e);
			if (body) handlers.stepProvider.setResult(body.result);
			break;
		}
		case "gaffer/stepWarning": {
			const body = parseDapBody(StepWarningBodySchema, e);
			// A runtime quirk that fired mid-event; attaches to the current
			// step like a log. Not the Problems panel - quirks are
			// value-dependent and have no source range.
			if (body) handlers.stepProvider.addWarning(body.code, body.message);
			break;
		}
		case "gaffer/stepError": {
			const body = parseDapBody(StepErrorBodySchema, e);
			// Tree item is sufficient signal for per-event handler errors.
			// Catastrophic failures come via fatal_error -> Diagnostic.
			if (body) handlers.stepProvider.setError(body.code, body.description);
			break;
		}
		case "gaffer/state": {
			const body = parseDapBody(StateBodySchema, e);
			if (body) handlers.stateProvider.updateFromState(body);
			break;
		}
		case "gaffer/finalState": {
			const body = parseDapBody(FinalStateBodySchema, e);
			if (body) handlers.stateProvider.hydrateFinalState(body);
			break;
		}
		case "gaffer/mode": {
			const body = parseDapBody(ModeBodySchema, e);
			if (body) {
				await handlers.setEngineMode(
					body.mode === "inspect" ? "inspecting" : "running",
				);
			}
			break;
		}
		case "gaffer/stats": {
			const body = parseDapBody(StatsBodySchema, e);
			if (body) {
				handlers.statusProvider.setStats(body.handled, body.errors);
				// Live mode omits both fields entirely; only forward when
				// the CLI is in fixture mode (a previously-set count clears
				// via reset() at session start, not mid-stream).
				if (body.skipped !== undefined || body.skippedByReason !== undefined) {
					handlers.statusProvider.setSkipped(
						body.skipped ?? 0,
						body.skippedByReason ?? {},
					);
				}
			}
			break;
		}
		case "gaffer/caughtUp": {
			const body = parseDapBody(CaughtUpBodySchema, e);
			if (body) handlers.phaseTracker.setCaughtUp(body.caughtUp);
			break;
		}
	}
}

// Validate a DAP custom-event body against a schema. On parse failure
// log the event name and issues, return undefined so the caller skips
// the dispatch. Keeps malformed events from corrupting state.
function parseDapBody<TSchema extends v.GenericSchema>(
	schema: TSchema,
	event: vscode.DebugSessionCustomEvent,
): v.InferOutput<TSchema> | undefined {
	const result = v.safeParse(schema, event.body);
	if (result.success) return result.output;
	log(
		`Malformed DAP event ${event.event}: ${result.issues
			.map((i) => i.message)
			.join("; ")}`,
	);
	return undefined;
}
