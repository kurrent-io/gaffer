import { describe, it, expect } from "vitest";
import {
	ProjectionSession,
	GafferError,
	InvalidProjectionError,
	CompilationTimeoutError,
	ProjectionHandlerError,
	ExecutionTimeoutError,
	MalformedEventError,
	StateSerializationError,
	ProjectionTransformError,
} from "../src/index.js";

const event = {
	eventType: "Test",
	streamId: "s-1",
	sequenceNumber: 42,
	data: "{}",
	isJson: true,
	eventId: "00000000-0000-0000-0000-000000000000",
	timestamp: "2026-01-01T00:00:00Z",
};

describe("Error types", () => {
	it("InvalidProjectionError - parse error with location", () => {
		const source = "this is not valid {{{{";
		try {
			new ProjectionSession(source);
			expect.fail("Expected error");
		} catch (err) {
			expect(err).toBeInstanceOf(InvalidProjectionError);
			expect(err).toBeInstanceOf(GafferError);
			const e = err as InvalidProjectionError;
			expect(e.code).toBe("invalid-projection");
			expect(e.description).toBeTruthy();
			expect(e.source).toBe(source);
			expect(e.location).toBeDefined();
			expect(e.location!.line).toBeGreaterThan(0);
			expect(e.location!.column).toBeGreaterThanOrEqual(0);
			expect(e.message).toMatchInlineSnapshot(`
				"Failed to compile projection

				error: Unexpected identifier 'is' (<anonymous>:1:6)
				    ┌─ 1:6
				    │
				  1 │ this is not valid {{{{
				    │      ^ Unexpected identifier 'is' (<anonymous>:1:6)
				    │
				"
			`);
		}
	});

	it("InvalidProjectionError - source definition error without location", () => {
		try {
			new ProjectionSession("fromStream(123)");
			expect.fail("Expected error");
		} catch (err) {
			expect(err).toBeInstanceOf(InvalidProjectionError);
			const e = err as InvalidProjectionError;
			expect(e.code).toBe("invalid-projection");
			expect(e.description).toBe("fromStream expects a string argument");
			expect(e.source).toBe("fromStream(123)");
			// TODO: source definition errors should have location info
			// (find function call in source via regex, multi-caret under function name)
			expect(e.location).toBeUndefined();
			expect(e.message).toMatchInlineSnapshot(`
				"Invalid projection definition

				error: fromStream expects a string argument
				"
			`);
		}
	});

	it("CompilationTimeoutError", () => {
		try {
			new ProjectionSession("while(true) {}", {
				compilationTimeoutMs: 100,
			});
			expect.fail("Expected error");
		} catch (err) {
			expect(err).toBeInstanceOf(CompilationTimeoutError);
			expect(err).toBeInstanceOf(GafferError);
			const e = err as CompilationTimeoutError;
			expect(e.code).toBe("compilation-timeout");
			expect(e.description).toContain("compile");
			expect(e.elapsed).toBeGreaterThan(0);
			expect(e.allowed).toBe(100);
			expect(e.message).toMatchInlineSnapshot(
				`"Projection script took too long to compile"`,
			);
		}
	});

	it("ProjectionHandlerError - with event context", () => {
		const source = `fromAll().when({
	$init: function() { return {}; },
	Test: function(s, e) { throw new Error("boom"); }
})`;
		const session = new ProjectionSession(source);
		try {
			session.feed(event);
			expect.fail("Expected error");
		} catch (err) {
			expect(err).toBeInstanceOf(ProjectionHandlerError);
			expect(err).toBeInstanceOf(GafferError);
			const e = err as ProjectionHandlerError;
			expect(e.code).toBe("handler-error");
			expect(e.description).toBe("boom");
			expect(e.source).toBe(source);
			expect(e.event.eventType).toBe("Test");
			expect(e.event.streamId).toBe("s-1");
			expect(e.event.sequenceNumber).toBe(42);
			expect(e.event.partition).toBeUndefined();
			expect(e.message).toMatchInlineSnapshot(`
				"Error processing event

				error: boom
				    ┌─ 3:31
				    │
				  1 │ fromAll().when({
				  2 │ 	$init: function() { return {}; },
				  3 │ 	Test: function(s, e) { throw new Error("boom"); }
				    │ 	                             ^ boom
				    │
				  at Test (<anonymous>:3:31)
				  at <anonymous>:3:8

				Event: 42@s-1
				Type:  Test
				"
			`);
		} finally {
			session.dispose();
		}
	});

	it("ProjectionHandlerError - with partition", () => {
		const source = `fromAll().foreachStream().when({
	$init: function() { return {}; },
	Test: function(s, e) { throw "fail"; }
})`;
		const session = new ProjectionSession(source);
		try {
			session.feed(event);
			expect.fail("Expected error");
		} catch (err) {
			const e = err as ProjectionHandlerError;
			expect(e.event.partition).toBe("s-1");
			expect(e.message).toMatchInlineSnapshot(`
				"Error processing event

				error: fail
				    ┌─ 3:31
				    │
				  1 │ fromAll().foreachStream().when({
				  2 │ 	$init: function() { return {}; },
				  3 │ 	Test: function(s, e) { throw "fail"; }
				    │ 	                             ^ fail
				    │
				  at Test (<anonymous>:3:31)
				  at <anonymous>:3:8

				Event: 42@s-1
				Type:  Test
				Partition: s-1
				"
			`);
		} finally {
			session.dispose();
		}
	});

	it("ExecutionTimeoutError", () => {
		const source = `fromAll().when({
			$init: function() { return {}; },
			Test: function(s, e) { while(true) {} }
		})`;
		const session = new ProjectionSession(source, {
			executionTimeoutMs: 100,
		});
		try {
			session.feed(event);
			expect.fail("Expected error");
		} catch (err) {
			expect(err).toBeInstanceOf(ExecutionTimeoutError);
			expect(err).toBeInstanceOf(GafferError);
			const e = err as ExecutionTimeoutError;
			expect(e.code).toBe("execution-timeout");
			expect(e.description).toContain("execute");
			expect(e.elapsed).toBeGreaterThan(0);
			expect(e.allowed).toBe(100);
			expect(e.event.eventType).toBe("Test");
			expect(e.event.streamId).toBe("s-1");
			expect(e.event.sequenceNumber).toBe(42);
			expect(e.message).toMatchInlineSnapshot(
				`"Projection script took too long to execute"`,
			);
		} finally {
			session.dispose();
		}
	});

	it("MalformedEventError - isJson with invalid data", () => {
		const source = `fromAll().when({
			$init: function() { return {}; },
			Test: function(s, e) { return e.data; }
		})`;
		const session = new ProjectionSession(source);
		try {
			session.feed({ ...event, data: "not json" });
			expect.fail("Expected error");
		} catch (err) {
			expect(err).toBeInstanceOf(MalformedEventError);
			expect(err).toBeInstanceOf(GafferError);
			const e = err as MalformedEventError;
			expect(e.code).toBe("malformed-event");
			expect(e.description).toContain("not valid JSON");
			expect(e.event.eventType).toBe("Test");
			expect(e.event.streamId).toBe("s-1");
			expect(e.event.sequenceNumber).toBe(42);
			expect(e.message).toMatchInlineSnapshot(`"Event data is not valid JSON"`);
		} finally {
			session.dispose();
		}
	});

	it("StateSerializationError - NaN in state", () => {
		const source = `fromAll().when({
			$init: function() { return {}; },
			Test: function(s, e) { s.value = NaN; return s; }
		})`;
		const session = new ProjectionSession(source);
		try {
			session.feed(event);
			expect.fail("Expected error");
		} catch (err) {
			expect(err).toBeInstanceOf(StateSerializationError);
			expect(err).toBeInstanceOf(GafferError);
			const e = err as StateSerializationError;
			expect(e.code).toBe("state-serialization-error");
			expect(e.description).toContain("NaN");
			expect(e.event.eventType).toBe("Test");
			expect(e.event.streamId).toBe("s-1");
			expect(e.event.sequenceNumber).toBe(42);
			expect(e.message).toMatchInlineSnapshot(
				`"Cannot serialize NaN as JSON value"`,
			);
		} finally {
			session.dispose();
		}
	});

	it("ProjectionTransformError", () => {
		const source = `fromAll().when({
			$init: function() { return {}; },
			Test: function(s, e) { return s; }
		}).transformBy(function(s) {
			throw new Error("transform failed");
		}).outputState()`;
		const session = new ProjectionSession(source);
		try {
			session.feed(event);
			session.getResult();
			expect.fail("Expected error");
		} catch (err) {
			expect(err).toBeInstanceOf(ProjectionTransformError);
			expect(err).toBeInstanceOf(GafferError);
			const e = err as ProjectionTransformError;
			expect(e.code).toBe("projection-transform-error");
			expect(e.description).toBe("transform failed");
			expect(e.source).toBe(source);
			expect(e.message).toMatchInlineSnapshot(`
				"Transform error

				error: transform failed
				    ┌─ 5:8
				    │
				  3 │ 	Test: function(s, e) { return s; }
				  4 │ }).transformBy(function(s) {
				  5 │ 	throw new Error("transform failed");
				    │ 	      ^ transform failed
				    │
				  at <anonymous>:5:10
				  at <anonymous>:6:18
				"
			`);
		} finally {
			session.dispose();
		}
	});
});
