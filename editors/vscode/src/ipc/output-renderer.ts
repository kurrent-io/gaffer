// Pretty-print a CliMessage to the Gaffer output channel via the
// module-level writeOutput singleton. Pure dispatch over the
// discriminated union; no state.

import { writeOutput } from "../output.js";
import type { CliMessage } from "./schemas.js";

export function renderCliMessage(msg: CliMessage): void {
	switch (msg.type) {
		case "info": {
			const p = msg.projection;
			writeOutput(p.name);
			if (p.source) writeOutput(`  Source: ${p.source}`);
			if (p.partitioning) writeOutput(`  Partitioning: ${p.partitioning}`);
			if (p.events) writeOutput(`  Events: ${p.events.join(", ")}`);
			if (p.engineVersion != null) writeOutput(`  Engine: v${p.engineVersion}`);
			writeOutput("");
			break;
		}
		case "event":
			writeOutput(`${msg.sequenceNumber}@${msg.streamId} ${msg.eventType}`);
			break;
		case "result":
			if (msg.status === "processed") {
				const partition = msg.partition ? ` [${msg.partition}]` : "";
				writeOutput(`  -> processed${partition}`);
				if (msg.logs?.length) {
					for (const l of msg.logs) writeOutput(`  [log] ${l}`);
				}
			} else {
				writeOutput(`  -> ${msg.status}: ${msg.reason}`);
			}
			break;
		case "error":
			writeOutput(`  ERROR: ${msg.code} - ${msg.description}`);
			break;
		case "summary":
			writeOutput("");
			writeOutput(
				`Summary: ${msg.handled} handled, ${msg.skipped} skipped, ${msg.errors} errors`,
			);
			break;
		case "debug":
			break;
		case "fatal_error": {
			const where = msg.file
				? `${msg.file}${msg.line != null ? `:${msg.line}` : ""}${
						msg.column != null ? `:${msg.column}` : ""
					}`
				: "";
			const eventSuffix = msg.eventId ? ` (event ${msg.eventId})` : "";
			writeOutput(
				`  FATAL: ${msg.code}${eventSuffix}: ${msg.description}${where ? ` at ${where}` : ""}`,
			);
			if (msg.jsStack) {
				for (const line of msg.jsStack.split(/\r?\n/)) {
					if (line) writeOutput(`    ${line}`);
				}
			}
			break;
		}
		case "exit":
			writeOutput(`Process exited (code ${msg.code})`);
			break;
		default: {
			// Exhaustiveness check: a new CliMessage variant added to schemas.ts
			// without a matching case here is a TS error.
			const _exhaustive: never = msg;
			void _exhaustive;
		}
	}
}
