import { describe, expect, it } from "vitest";
import { renderCliMessage } from "./output-renderer.js";
import type { CliMessage } from "./schemas.js";

// Pure transform tests. Each variant is rendered into a string buffer
// and asserted against the lines we expect. Capture the *content* (one
// of these may be the user's only readable trace of a failed run) and
// the *separator* contract (info ends in a blank line, summary is
// preceded by one) since both serve as visual grouping in the output
// channel.

function record(msg: CliMessage): string[] {
	const lines: string[] = [];
	renderCliMessage(msg, (line) => lines.push(line));
	return lines;
}

describe("renderCliMessage", () => {
	it("renders an info with all fields and trailing blank line", () => {
		const lines = record({
			type: "info",
			projection: {
				name: "checkout",
				source: "all",
				partitioning: "by stream",
				events: ["A", "B"],
				engineVersion: 2,
			},
		});
		expect(lines).toEqual([
			"checkout",
			"  Source: all",
			"  Partitioning: by stream",
			"  Events: A, B",
			"  Engine: v2",
			"",
		]);
	});

	it("omits absent info fields but still ends with a blank line", () => {
		const lines = record({
			type: "info",
			projection: { name: "checkout" },
		});
		expect(lines).toEqual(["checkout", ""]);
	});

	it("renders an event line", () => {
		const lines = record({
			type: "event",
			sequenceNumber: 7,
			streamId: "orders-1",
			eventType: "OrderPlaced",
		});
		expect(lines).toEqual(["7@orders-1 OrderPlaced"]);
	});

	it("renders a processed result with partition and logs", () => {
		const lines = record({
			type: "result",
			status: "processed",
			partition: "p1",
			logs: ["hello", "world"],
		});
		expect(lines).toEqual([
			"  -> processed [p1]",
			"  [log] hello",
			"  [log] world",
		]);
	});

	it("renders a processed result without partition or logs", () => {
		const lines = record({ type: "result", status: "processed" });
		expect(lines).toEqual(["  -> processed"]);
	});

	it("renders a skipped result", () => {
		const lines = record({
			type: "result",
			status: "skipped",
			reason: "no match",
		});
		expect(lines).toEqual(["  -> skipped: no match"]);
	});

	it("renders an error", () => {
		const lines = record({
			type: "error",
			code: "E_FOO",
			description: "boom",
		});
		expect(lines).toEqual(["  ERROR: E_FOO - boom"]);
	});

	it("renders a summary preceded by a blank line", () => {
		const lines = record({
			type: "summary",
			handled: 3,
			skipped: 1,
			errors: 0,
		});
		expect(lines).toEqual(["", "Summary: 3 handled, 1 skipped, 0 errors"]);
	});

	it("emits nothing for a debug message", () => {
		const lines = record({ type: "debug", port: 4711 });
		expect(lines).toEqual([]);
	});

	it("renders a fatal_error with file:line:column and stack", () => {
		const lines = record({
			type: "fatal_error",
			code: "JS_ERROR",
			description: "bad",
			file: "/p/projection.js",
			line: 12,
			column: 4,
			jsStack: "at handler\nat caller",
			eventId: "evt-1",
		});
		expect(lines).toEqual([
			"  FATAL: JS_ERROR (event evt-1): bad at /p/projection.js:12:4",
			"    at handler",
			"    at caller",
		]);
	});

	it("renders a fatal_error without a file", () => {
		const lines = record({
			type: "fatal_error",
			code: "JS_ERROR",
			description: "bad",
		});
		expect(lines).toEqual(["  FATAL: JS_ERROR: bad"]);
	});

	it("strips empty stack lines from a jsStack", () => {
		const lines = record({
			type: "fatal_error",
			code: "JS_ERROR",
			description: "bad",
			jsStack: "at a\n\nat b\n",
		});
		expect(lines).toEqual(["  FATAL: JS_ERROR: bad", "    at a", "    at b"]);
	});

	it("renders the synthetic exit message", () => {
		const lines = record({ type: "exit", code: 0 });
		expect(lines).toEqual(["Process exited (code 0)"]);
	});

	it("renders exit with a null code", () => {
		const lines = record({ type: "exit", code: null });
		expect(lines).toEqual(["Process exited (code null)"]);
	});
});
