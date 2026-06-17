// Pretty-print a CliMessage line-by-line through a write callback.
// Pure dispatch over the discriminated union; no state. The `write`
// injection makes this testable in isolation - production callers
// pass writeOutput from output.ts; tests pass a recording fake.

import type { CliMessage } from "./schemas.js";

type WriteFn = (line: string) => void;

export function renderCliMessage(msg: CliMessage, write: WriteFn): void {
	switch (msg.type) {
		case "info": {
			const p = msg.projection;
			write(p.name);
			if (p.source) write(`  Source: ${p.source}`);
			if (p.partitioning) write(`  Partitioning: ${p.partitioning}`);
			if (p.events) write(`  Events: ${p.events.join(", ")}`);
			if (p.engineVersion != null) write(`  Engine: v${p.engineVersion}`);
			write("");
			break;
		}
		case "event":
			write(`${msg.sequenceNumber}@${msg.streamId} ${msg.eventType}`);
			break;
		case "result":
			if (msg.status === "processed") {
				const partition = msg.partition ? ` [${msg.partition}]` : "";
				write(`  -> processed${partition}`);
				if (msg.logs?.length) {
					for (const l of msg.logs) write(`  [log] ${l}`);
				}
			} else {
				write(`  -> ${msg.status}: ${msg.reason}`);
			}
			break;
		case "error":
			write(`  ERROR: ${msg.code} - ${msg.description}`);
			break;
		case "summary":
			write("");
			write(
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
			write(
				`  FATAL: ${msg.code}${eventSuffix}: ${msg.description}${where ? ` at ${where}` : ""}`,
			);
			if (msg.jsStack) {
				for (const line of msg.jsStack.split(/\r?\n/)) {
					if (line) write(`    ${line}`);
				}
			}
			break;
		}
		case "auth_required":
			write(
				`  Authentication required for env ${msg.env} - run gaffer auth --env ${msg.env}`,
			);
			break;
		case "exit":
			write(`Process exited (code ${msg.code})`);
			break;
		default: {
			// Exhaustiveness check: a new CliMessage variant added to schemas.ts
			// without a matching case here is a TS error.
			const _exhaustive: never = msg;
			void _exhaustive;
		}
	}
}
