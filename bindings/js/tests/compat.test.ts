import { describe, it, expect } from "vitest";
import {
	ProjectionSession,
	ProjectionHandlerError,
	InvalidArgumentError,
} from "../src/index.js";

const testEvent = {
	eventType: "Test",
	streamId: "s-1",
	sequenceNumber: 42,
	data: "{}",
	isJson: true,
	eventId: "00000000-0000-0000-0000-000000000000",
	created: "2026-01-01T00:00:00Z",
};

describe("quirksVersion option", () => {
	it("accepts a valid version", () => {
		const session = new ProjectionSession(
			"fromAll().when({ $any: function (s, e) { return s; } });",
			{ engineVersion: 2, quirksVersion: "26.1.0" },
		);
		session.dispose();
	});

	it("rejects a malformed version", () => {
		expect(
			() =>
				new ProjectionSession("fromAll()", {
					engineVersion: 2,
					quirksVersion: "not-a-version",
				}),
		).toThrow(InvalidArgumentError);
		try {
			new ProjectionSession("fromAll()", {
				engineVersion: 2,
				quirksVersion: "not-a-version",
			});
		} catch (err) {
			expect(err).toBeInstanceOf(InvalidArgumentError);
			expect((err as InvalidArgumentError).field).toBe("quirksVersion");
		}
	});
});

describe("compatCode propagation", () => {
	it("compat-firing throw carries the quirk code", () => {
		// 3-arg linkStreamTo is the always-quirkgy path: throws and the
		// runtime stamps the error with quirk.linkStreamTo.outOfBoundsParameters.
		const source = `fromAll().when({
			$any: function (s, e) {
				linkStreamTo("a", e.streamId, { reason: "x" });
				return s;
			}
		})`;
		const session = new ProjectionSession(source, { engineVersion: 2 });
		try {
			expect(() => session.feed(testEvent)).toThrow(ProjectionHandlerError);
			try {
				session.feed(testEvent);
			} catch (err) {
				expect(err).toBeInstanceOf(ProjectionHandlerError);
				expect((err as ProjectionHandlerError).compatCode).toBe(
					"quirk.linkStreamTo.outOfBoundsParameters",
				);
				// The throwing quirk also reaches the diagnostics channel.
				const diagnostics = (err as ProjectionHandlerError).diagnostics ?? [];
				expect(
					diagnostics.some(
						(d) => d.code === "quirk.linkStreamTo.outOfBoundsParameters",
					),
				).toBe(true);
			}
		} finally {
			session.dispose();
		}
	});
});

describe("V2 transform diagnostics", () => {
	it("emits usage.transforms.notInvoked for transformBy under V2", () => {
		// Cross-binding regression test: a runtime regression that drops
		// the diagnostic, or a serialization regression in the wire format,
		// would only show up here.
		const session = new ProjectionSession(
			`fromAll().when({ $any: function (s, e) { return s; } }).transformBy(function (s) { return s; });`,
			{ engineVersion: 2 },
		);
		try {
			const diagnostics = session.getSources().diagnostics;
			if (diagnostics === null) {
				expect.fail("expected diagnostics, got null");
			}
			expect(
				diagnostics.some((d) => d.code === "usage.transforms.notInvoked"),
			).toBe(true);
		} finally {
			session.dispose();
		}
	});

	it("emits usage.outputState.unconditional for outputState() under V2", () => {
		const session = new ProjectionSession(
			`fromAll().when({ $any: function (s, e) { return s; } }).outputState();`,
			{ engineVersion: 2 },
		);
		try {
			const diagnostics = session.getSources().diagnostics;
			if (diagnostics === null) {
				expect.fail("expected diagnostics, got null");
			}
			expect(
				diagnostics.some((d) => d.code === "usage.outputState.unconditional"),
			).toBe(true);
		} finally {
			session.dispose();
		}
	});
});
