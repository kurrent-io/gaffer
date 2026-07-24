import { render } from "@solidjs/testing-library";
import { ErrorBoundary, type JSX } from "solid-js";
import { describe, expect, it, vi } from "vitest";
import { errorToMessage, installErrorReporting } from "./report-errors";

describe("errorToMessage", () => {
	it("carries an Error's name/message/stack", () => {
		const err = new TypeError("nope");
		expect(errorToMessage(err)).toMatchObject({
			command: "error",
			name: "TypeError",
			message: "nope",
		});
	});
	it("wraps a non-Error reason", () => {
		expect(errorToMessage("boom")).toMatchObject({
			command: "error",
			name: "Error",
			message: "boom",
		});
	});
});

describe("installErrorReporting", () => {
	it("reports window errors and rejections, and skips content-less error events", () => {
		const report = vi.fn();
		installErrorReporting(report);

		window.dispatchEvent(
			Object.assign(new Event("error"), {
				error: new Error("boom"),
				message: "boom",
			}),
		);
		expect(report).toHaveBeenCalledTimes(1);
		expect(report.mock.calls[0]?.[0]).toMatchObject({
			command: "error",
			message: "boom",
		});

		window.dispatchEvent(
			Object.assign(new Event("unhandledrejection"), {
				reason: new Error("rejected"),
			}),
		);
		expect(report).toHaveBeenCalledTimes(2);

		// Resource-load failure: neither .error nor .message - no report.
		window.dispatchEvent(new Event("error"));
		expect(report).toHaveBeenCalledTimes(2);
	});
});

describe("ErrorBoundary fallback", () => {
	it("posts exactly once for a render error", () => {
		const report = vi.fn();
		function Boom(): JSX.Element {
			throw new Error("render boom");
		}
		render(() => (
			<ErrorBoundary
				fallback={(err) => {
					report(errorToMessage(err));
					return <div>failed</div>;
				}}
			>
				<Boom />
			</ErrorBoundary>
		));
		expect(report).toHaveBeenCalledTimes(1);
		expect(report.mock.calls[0]?.[0]).toMatchObject({
			command: "error",
			message: "render boom",
		});
	});
});
