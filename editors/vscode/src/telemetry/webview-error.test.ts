import { describe, expect, it } from "vitest";
import type { Phase } from "./exception.js";
import { reportWebviewError } from "./webview-error.js";

function fakeTelemetry() {
	const calls: { phase: Phase; err: unknown }[] = [];
	return {
		telemetry: {
			emit: () => {},
			drain: async () => {},
			refreshOptOut: async () => {},
			invokerId: () => null,
			isOptedOut: () => false,
			reportException: (phase: Phase, err: unknown) =>
				calls.push({ phase, err }),
		},
		calls,
	};
}

describe("reportWebviewError", () => {
	it("reports under the webview phase with a real Error carrying name/message/stack", () => {
		const { telemetry, calls } = fakeTelemetry();
		reportWebviewError(telemetry, {
			command: "error",
			name: "TypeError",
			message: "x is not a function",
			stack: "TypeError: x is not a function\n  at foo (status.js:1:2)",
		});
		expect(calls).toHaveLength(1);
		expect(calls[0]?.phase).toBe("webview");
		const err = calls[0]?.err;
		expect(err).toBeInstanceOf(Error);
		expect((err as Error).name).toBe("TypeError");
		expect((err as Error).message).toBe("x is not a function");
		expect((err as Error).stack).toContain("status.js:1:2");
	});
});
