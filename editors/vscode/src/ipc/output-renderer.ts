// Pretty-print a CliMessage to a vscode OutputChannel. Pure routing over
// the discriminated union; no state, no dependency on listener machinery.
//
// Lives separately from session.ts because the renderer doesn't share
// concerns with process management or listener dispatch - the output
// channel just happens to live in the same parent class today.

import type * as vscode from "vscode";
import type { CliMessage } from "./schemas.js";

export function renderCliMessage(
	output: vscode.OutputChannel,
	msg: CliMessage,
): void {
	switch (msg.type) {
		case "info": {
			const p = msg.projection;
			output.appendLine(p.name);
			if (p.source) output.appendLine(`  Source: ${p.source}`);
			if (p.partitioning)
				output.appendLine(`  Partitioning: ${p.partitioning}`);
			if (p.events) output.appendLine(`  Events: ${p.events.join(", ")}`);
			if (p.engineVersion != null)
				output.appendLine(`  Engine: v${p.engineVersion}`);
			output.appendLine("");
			break;
		}
		case "event":
			output.appendLine(
				`${msg.sequenceNumber}@${msg.streamId} ${msg.eventType}`,
			);
			break;
		case "result":
			if (msg.status === "processed") {
				const partition = msg.partition ? ` [${msg.partition}]` : "";
				output.appendLine(`  -> processed${partition}`);
				if (msg.logs?.length) {
					for (const l of msg.logs) output.appendLine(`  [log] ${l}`);
				}
			} else {
				output.appendLine(`  -> ${msg.status}: ${msg.reason}`);
			}
			break;
		case "error":
			output.appendLine(`  ERROR: ${msg.code} - ${msg.description}`);
			break;
		case "summary":
			output.appendLine("");
			output.appendLine(
				`Summary: ${msg.handled} handled, ${msg.skipped} skipped, ${msg.errors} errors`,
			);
			break;
		case "debug":
			break;
		case "exit":
			output.appendLine(`Process exited (code ${msg.code})`);
			break;
		default: {
			// Exhaustiveness check: a new CliMessage variant added to schemas.ts
			// without a matching case here is a TS error.
			const _exhaustive: never = msg;
			void _exhaustive;
		}
	}
}
