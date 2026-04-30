// Pure routing for DAP custom events emitted by the gaffer CLI's DAP
// server. Each event body is validated against its schema; on parse
// failure the dispatch is skipped and the issue is logged.
//
// Kept separate from extension.ts so the switch can be reasoned about as
// a function of (event, providers, log) without sharing the activate()
// closure.

import * as vscode from "vscode";
import * as v from "valibot";
import {
	ModeBodySchema,
	StateBodySchema,
	StepEmitBodySchema,
	StepErrorBodySchema,
	StepLogBodySchema,
	StepResultBodySchema,
	StepStartBodySchema,
} from "./schemas.js";
import type { StateProvider } from "../panels/state.js";
import type { StepProvider } from "../panels/step.js";

export interface DapHandlers {
	stepProvider: StepProvider;
	stateProvider: StateProvider;
	setInspecting: (inspecting: boolean) => Promise<void> | void;
	log: (msg: string) => void;
}

export async function dispatchDapEvent(
	e: vscode.DebugSessionCustomEvent,
	handlers: DapHandlers,
): Promise<void> {
	if (e.session.type !== "gaffer") return;
	handlers.stateProvider.setDebugSession(e.session);

	switch (e.event) {
		case "gaffer/stepStart": {
			const body = parseDapBody(StepStartBodySchema, e, handlers.log);
			if (body) handlers.stepProvider.startStep(body.event);
			break;
		}
		case "gaffer/stepLog": {
			const body = parseDapBody(StepLogBodySchema, e, handlers.log);
			if (body) handlers.stepProvider.addLog(body.message);
			break;
		}
		case "gaffer/stepEmit": {
			const body = parseDapBody(StepEmitBodySchema, e, handlers.log);
			if (body) handlers.stepProvider.addEmit(body);
			break;
		}
		case "gaffer/stepResult": {
			const body = parseDapBody(StepResultBodySchema, e, handlers.log);
			if (body) handlers.stepProvider.setResult(body.result);
			break;
		}
		case "gaffer/stepError": {
			const body = parseDapBody(StepErrorBodySchema, e, handlers.log);
			if (body) {
				handlers.stepProvider.setError(body.code, body.description);
				await vscode.window.showErrorMessage(
					`Gaffer: ${body.code} - ${body.description}`,
				);
			}
			break;
		}
		case "gaffer/state": {
			const body = parseDapBody(StateBodySchema, e, handlers.log);
			if (body) handlers.stateProvider.updateFromState(body);
			break;
		}
		case "gaffer/mode": {
			const body = parseDapBody(ModeBodySchema, e, handlers.log);
			if (body) await handlers.setInspecting(body.mode === "inspect");
			break;
		}
	}
}

// Validate a DAP custom-event body against a schema. On parse failure log
// the event name and issues, return undefined so the caller skips the
// dispatch. Keeps malformed events from corrupting state.
function parseDapBody<TSchema extends v.GenericSchema>(
	schema: TSchema,
	event: vscode.DebugSessionCustomEvent,
	log: (msg: string) => void,
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
