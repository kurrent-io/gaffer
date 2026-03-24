package gafferruntime

import (
	"errors"
	"testing"
)

const testEvent = `{"eventType":"Test","streamId":"s-1","sequenceNumber":42,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`

func TestError_InvalidProjection_ParseError(t *testing.T) {
	source := "this is not valid {{{{"
	_, err := NewSession(source, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var e *InvalidProjectionError
	if !errors.As(err, &e) {
		t.Fatalf("expected InvalidProjectionError, got %T", err)
	}
	assertEqual(t, "code", "invalid-projection", e.ErrorCode())
	assertNotEmpty(t, "description", e.Desc)
	assertEqual(t, "source", source, e.Source)
	assertNotNil(t, "location", e.Location)
	assertPositive(t, "line", e.Location.Line)
	assertContains(t, "message", e.Error(), "Failed to compile projection")
	assertContains(t, "message", e.Error(), "┌─")
	assertContains(t, "message", e.Error(), "^")
}

func TestError_InvalidProjection_SourceDefinition(t *testing.T) {
	_, err := NewSession("fromStream(123)", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var e *InvalidProjectionError
	if !errors.As(err, &e) {
		t.Fatalf("expected InvalidProjectionError, got %T", err)
	}
	assertEqual(t, "code", "invalid-projection", e.ErrorCode())
	assertEqual(t, "description", "fromStream expects a string argument", e.Desc)
	assertNil(t, "location", e.Location)
	assertEqual(t, "message", "Invalid projection definition\n\nerror: fromStream expects a string argument\n", e.Error())
}

func TestError_CompilationTimeout(t *testing.T) {
	opts := `{"compilationTimeoutMs":100}`
	_, err := NewSession("while(true) {}", &opts)
	if err == nil {
		t.Fatal("expected error")
	}
	var e *CompilationTimeoutError
	if !errors.As(err, &e) {
		t.Fatalf("expected CompilationTimeoutError, got %T", err)
	}
	assertEqual(t, "code", "compilation-timeout", e.ErrorCode())
	assertContains(t, "description", e.Desc, "compile")
	assertPositive(t, "elapsed", e.ElapsedMs)
	assertIntEqual(t, "allowed", 100, e.AllowedMs)
	assertContains(t, "message", e.Error(), "100ms limit")
}

func TestError_ProjectionHandler(t *testing.T) {
	source := "fromAll().when({\n\t$init: function() { return {}; },\n\tTest: function(s, e) { throw new Error(\"boom\"); }\n})"
	session, err := NewSession(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Destroy()

	_, err = session.Feed(testEvent)
	if err == nil {
		t.Fatal("expected error")
	}
	var e *ProjectionHandlerError
	if !errors.As(err, &e) {
		t.Fatalf("expected ProjectionHandlerError, got %T", err)
	}
	assertEqual(t, "code", "handler-error", e.ErrorCode())
	assertEqual(t, "description", "boom", e.Desc)
	assertEqual(t, "source", source, e.Source)
	assertEqual(t, "event.eventType", "Test", e.Event.EventType)
	assertEqual(t, "event.streamId", "s-1", e.Event.StreamID)
	assertInt64(t, "event.sequenceNumber", 42, e.Event.SequenceNumber)
	assertEqual(t, "event.partition", "", e.Event.Partition)
	assertContains(t, "message", e.Error(), "Error in 'Test' handler")
	assertContains(t, "message", e.Error(), "Handler threw: boom")
	assertContains(t, "message", e.Error(), "Event: 42@s-1")
	assertContains(t, "message", e.Error(), "Type:  Test")
}

func TestError_ProjectionHandler_WithPartition(t *testing.T) {
	source := "fromAll().foreachStream().when({\n\t$init: function() { return {}; },\n\tTest: function(s, e) { throw \"fail\"; }\n})"
	session, err := NewSession(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Destroy()

	_, err = session.Feed(testEvent)
	if err == nil {
		t.Fatal("expected error")
	}
	var e *ProjectionHandlerError
	if !errors.As(err, &e) {
		t.Fatalf("expected ProjectionHandlerError, got %T", err)
	}
	assertEqual(t, "event.partition", "s-1", e.Event.Partition)
	assertContains(t, "message", e.Error(), "Partition: s-1")
}

func TestError_ExecutionTimeout(t *testing.T) {
	opts := `{"executionTimeoutMs":100}`
	source := "fromAll().when({\n\t$init: function() { return {}; },\n\tTest: function(s, e) { while(true) {} }\n})"
	session, err := NewSession(source, &opts)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Destroy()

	_, err = session.Feed(testEvent)
	if err == nil {
		t.Fatal("expected error")
	}
	var e *ExecutionTimeoutError
	if !errors.As(err, &e) {
		t.Fatalf("expected ExecutionTimeoutError, got %T", err)
	}
	assertEqual(t, "code", "execution-timeout", e.ErrorCode())
	assertContains(t, "description", e.Desc, "execute")
	assertPositive(t, "elapsed", e.ElapsedMs)
	assertIntEqual(t, "allowed", 100, e.AllowedMs)
	assertEqual(t, "event.eventType", "Test", e.Event.EventType)
	assertEqual(t, "event.streamId", "s-1", e.Event.StreamID)
	assertInt64(t, "event.sequenceNumber", 42, e.Event.SequenceNumber)
	assertContains(t, "message", e.Error(), "100ms limit")
	assertContains(t, "message", e.Error(), "Event: 42@s-1")
}

func TestError_MalformedEvent(t *testing.T) {
	source := "fromAll().when({\n\t$init: function() { return {}; },\n\tTest: function(s, e) { return e.data; }\n})"
	session, err := NewSession(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Destroy()

	_, err = session.Feed(`{"eventType":"Test","streamId":"s-1","sequenceNumber":42,"data":"not json","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)
	if err == nil {
		t.Fatal("expected error")
	}
	var e *MalformedEventError
	if !errors.As(err, &e) {
		t.Fatalf("expected MalformedEventError, got %T", err)
	}
	assertEqual(t, "code", "malformed-event", e.ErrorCode())
	assertContains(t, "description", e.Desc, "not valid JSON")
	assertEqual(t, "event.eventType", "Test", e.Event.EventType)
	assertEqual(t, "event.streamId", "s-1", e.Event.StreamID)
	assertInt64(t, "event.sequenceNumber", 42, e.Event.SequenceNumber)
	assertContains(t, "message", e.Error(), "Event data is not valid JSON")
	assertContains(t, "message", e.Error(), "Event: 42@s-1")
}

func TestError_StateSerialization_NaN(t *testing.T) {
	source := "fromAll().when({\n\t$init: function() { return {}; },\n\tTest: function(s, e) { s.value = NaN; return s; }\n})"
	session, err := NewSession(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Destroy()

	_, err = session.Feed(testEvent)
	if err == nil {
		t.Fatal("expected error")
	}
	var e *StateSerializationError
	if !errors.As(err, &e) {
		t.Fatalf("expected StateSerializationError, got %T", err)
	}
	assertEqual(t, "code", "state-serialization-error", e.ErrorCode())
	assertContains(t, "description", e.Desc, "NaN")
	assertEqual(t, "event.eventType", "Test", e.Event.EventType)
	assertEqual(t, "event.streamId", "s-1", e.Event.StreamID)
	assertInt64(t, "event.sequenceNumber", 42, e.Event.SequenceNumber)
	assertContains(t, "message", e.Error(), "Failed to serialize projection state")
	assertContains(t, "message", e.Error(), "Event: 42@s-1")
}

func TestError_ProjectionTransform(t *testing.T) {
	source := "fromAll().when({\n\t$init: function() { return {}; },\n\tTest: function(s, e) { return s; }\n}).transformBy(function(s) {\n\tthrow new Error(\"transform failed\");\n}).outputState()"
	session, err := NewSession(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Destroy()

	_, err = session.Feed(testEvent)
	if err == nil {
		t.Fatal("expected error")
	}
	var e *ProjectionTransformError
	if !errors.As(err, &e) {
		t.Fatalf("expected ProjectionTransformError, got %T", err)
	}
	assertEqual(t, "code", "projection-transform-error", e.ErrorCode())
	assertEqual(t, "description", "transform failed", e.Desc)
	assertEqual(t, "source", source, e.Source)
	assertContains(t, "message", e.Error(), "Transform error")
	assertContains(t, "message", e.Error(), "transform failed")
}

// Test helpers

func assertEqual(t *testing.T, field, expected, actual string) {
	t.Helper()
	if expected != actual {
		t.Fatalf("%s: expected %q, got %q", field, expected, actual)
	}
}

func assertIntEqual(t *testing.T, field string, expected, actual int) {
	t.Helper()
	if expected != actual {
		t.Fatalf("%s: expected %d, got %d", field, expected, actual)
	}
}

func assertInt64(t *testing.T, field string, expected, actual int64) {
	t.Helper()
	if expected != actual {
		t.Fatalf("%s: expected %d, got %d", field, expected, actual)
	}
}

func assertContains(t *testing.T, field, haystack, needle string) {
	t.Helper()
	if len(haystack) == 0 || len(needle) == 0 {
		t.Fatalf("%s: empty string in contains check", field)
	}
	for i := range haystack {
		if len(haystack)-i < len(needle) {
			break
		}
		if haystack[i:i+len(needle)] == needle {
			return
		}
	}
	t.Fatalf("%s: expected to contain %q, got:\n%s", field, needle, haystack)
}

func assertNotEmpty(t *testing.T, field, value string) {
	t.Helper()
	if value == "" {
		t.Fatalf("%s: expected non-empty string", field)
	}
}

func assertPositive(t *testing.T, field string, value int) {
	t.Helper()
	if value <= 0 {
		t.Fatalf("%s: expected positive, got %d", field, value)
	}
}

func assertNotNil(t *testing.T, field string, value any) {
	t.Helper()
	if value == nil {
		t.Fatalf("%s: expected non-nil", field)
	}
}

func assertNil(t *testing.T, field string, value *JsLocation) {
	t.Helper()
	if value != nil {
		t.Fatalf("%s: expected nil, got %+v", field, value)
	}
}
