package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1000, "1,000"},
		{1247, "1,247"},
		{10000, "10,000"},
		{1000000, "1,000,000"},
	}
	for _, tt := range tests {
		got := formatNumber(tt.input)
		if got != tt.want {
			t.Errorf("formatNumber(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestHasContent(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
		want bool
	}{
		{"nil", nil, false},
		{"empty", json.RawMessage{}, false},
		{"null", json.RawMessage("null"), false},
		{"object", json.RawMessage(`{"a":1}`), true},
		{"string", json.RawMessage(`"hello"`), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasContent(tt.raw)
			if got != tt.want {
				t.Errorf("hasContent(%q) = %v, want %v", string(tt.raw), got, tt.want)
			}
		})
	}
}

func TestTextWriter_WriteInfo(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)

	info := gafferruntime.ProjectionInfo{
		AllStreams: true,
		ByStreams:  true,
		Events:     []string{"OrderPlaced", "OrderShipped"},
	}
	tw.WriteInfo("my-projection", info, 2)

	out := buf.String()
	testutil.AssertContains(t, out, "my-projection")
	testutil.AssertContains(t, out, "Source: $all")
	testutil.AssertContains(t, out, "Partitioning: per stream")
	testutil.AssertContains(t, out, "Events: OrderPlaced, OrderShipped")
	testutil.AssertContains(t, out, "Engine: v2")
}

func TestTextWriter_WriteInfo_BiStateAndProducesResults(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)

	info := gafferruntime.ProjectionInfo{
		AllStreams:      true,
		BiState:         true,
		ProducesResults: true,
	}
	tw.WriteInfo("bi-state-proj", info, 2)

	out := buf.String()
	testutil.AssertContains(t, out, "BiState: yes")
	testutil.AssertContains(t, out, "Produces results: yes")
}

func TestTextWriter_WriteInfo_OmitsFalseFlags(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)

	info := gafferruntime.ProjectionInfo{
		AllStreams: true,
	}
	tw.WriteInfo("simple-proj", info, 2)

	out := buf.String()
	if strings.Contains(out, "BiState") {
		t.Error("should not show BiState when false")
	}
	if strings.Contains(out, "Produces results") {
		t.Error("should not show Produces results when false")
	}
}

func TestTextWriter_WriteEvent(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)

	event := eventInfo{
		SequenceNumber: 1,
		StreamID:       "order-1",
		EventType:      "OrderPlaced",
		Data:           json.RawMessage(`{"amount":50}`),
		Metadata:       json.RawMessage(`{"corr":"abc"}`),
	}
	tw.WriteEvent(event)

	out := buf.String()
	testutil.AssertContains(t, out, "1@order-1")
	testutil.AssertContains(t, out, "type: OrderPlaced")
	testutil.AssertContains(t, out, "data: {\"amount\":50}")
	testutil.AssertContains(t, out, "metadata: {\"corr\":\"abc\"}")
}

func TestTextWriter_WriteResult_Processed(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)

	result := &gafferruntime.FeedResult{
		Status:    "processed",
		Partition: "order-1",
		State:     json.RawMessage(`{"count":1}`),
	}
	tw.WriteResult("1@order-1", result)

	out := buf.String()
	testutil.AssertContains(t, out, "partition: order-1\n")
	testutil.AssertContains(t, out, `state: {"count":1}`)
}

func TestTextWriter_WriteResult_Skipped(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)

	result := &gafferruntime.FeedResult{
		Status:     "skipped",
		SkipReason: "unhandled",
	}
	tw.WriteResult("1@order-1", result)

	out := buf.String()
	testutil.AssertContains(t, out, "skipped\n")
	testutil.AssertContains(t, out, "reason: unhandled")
}

func TestTextWriter_WriteSummary_Unpartitioned(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)

	stats := engine.EventStats{Handled: 42, Skipped: 0}
	state := engine.StateSummary{
		State: json.RawMessage(`{"count":42}`),
	}
	tw.WriteSummary(stats, state)

	out := buf.String()
	testutil.AssertContains(t, out, "42 events processed (42 handled, 0 skipped)")
	testutil.AssertContains(t, out, `State: {"count":42}`)
}

func TestTextWriter_WriteSummary_Partitioned(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)

	stats := engine.EventStats{Handled: 3, Skipped: 1}
	state := engine.StateSummary{
		Partitioned: true,
		Partitions: map[string]engine.PartitionState{
			"order-1": {State: json.RawMessage(`{"count":2}`)},
		},
	}
	tw.WriteSummary(stats, state)

	out := buf.String()
	testutil.AssertContains(t, out, "4 events processed (3 handled, 1 skipped)")
	testutil.AssertContains(t, out, "order-1")
	testutil.AssertContains(t, out, `state: {"count":2}`)
}

func TestJSONWriter_WriteEvent(t *testing.T) {
	var buf bytes.Buffer
	jw := newJSONWriter(&buf)

	event := eventInfo{
		SequenceNumber: 1,
		StreamID:       "order-1",
		EventType:      "OrderPlaced",
		Data:           json.RawMessage(`{"amount":50}`),
	}
	jw.WriteEvent(event)

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	testutil.AssertEqual(t, "type", "event", line["type"])
	testutil.AssertEqual(t, "id", "1@order-1", line["id"])
	testutil.AssertEqual(t, "eventType", "OrderPlaced", line["eventType"])
}

func TestJSONWriter_WriteResult_Processed(t *testing.T) {
	var buf bytes.Buffer
	jw := newJSONWriter(&buf)

	result := &gafferruntime.FeedResult{
		Status:    "processed",
		Partition: "order-1",
		State:     json.RawMessage(`{"count":1}`),
	}
	jw.WriteResult("1@order-1", result)

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	testutil.AssertEqual(t, "type", "result", line["type"])
	testutil.AssertEqual(t, "status", "processed", line["status"])
	testutil.AssertEqual(t, "partition", "order-1", line["partition"])

	if _, ok := line["emitted"]; !ok {
		t.Error("expected emitted field")
	}
	if _, ok := line["logs"]; !ok {
		t.Error("expected logs field")
	}
}

func TestJSONWriter_WriteResult_Skipped(t *testing.T) {
	var buf bytes.Buffer
	jw := newJSONWriter(&buf)

	result := &gafferruntime.FeedResult{
		Status:     "skipped",
		SkipReason: "unhandled",
	}
	jw.WriteResult("1@order-1", result)

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	testutil.AssertEqual(t, "status", "skipped", line["status"])
	testutil.AssertEqual(t, "reason", "unhandled", line["reason"])

	if _, ok := line["emitted"]; ok {
		t.Error("skipped result should not have emitted field")
	}
}

func TestJSONWriter_WriteSummary_Unpartitioned(t *testing.T) {
	var buf bytes.Buffer
	jw := newJSONWriter(&buf)

	stats := engine.EventStats{Handled: 10, Skipped: 2}
	state := engine.StateSummary{
		State: json.RawMessage(`{"count":10}`),
	}
	jw.WriteSummary(stats, state)

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	testutil.AssertEqual(t, "type", "summary", line["type"])
	testutil.AssertEqualFloat(t, "processed", 12, line["processed"])
	testutil.AssertEqualFloat(t, "handled", 10, line["handled"])
	testutil.AssertEqualFloat(t, "skipped", 2, line["skipped"])

	if _, ok := line["partitions"]; ok {
		t.Error("unpartitioned summary should not have partitions")
	}
}

func TestJSONWriter_WriteInfo(t *testing.T) {
	var buf bytes.Buffer
	jw := newJSONWriter(&buf)

	info := gafferruntime.ProjectionInfo{
		Categories: []string{"order"},
		ByStreams:  true,
		Events:     []string{"OrderPlaced"},
	}
	jw.WriteInfo("my-projection", info, 2)

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	testutil.AssertEqual(t, "type", "info", line["type"])

	proj, ok := line["projection"].(map[string]any)
	if !ok {
		t.Fatal("expected projection object")
	}

	testutil.AssertEqual(t, "name", "my-projection", proj["name"])
	testutil.AssertEqual(t, "source", "categories", proj["source"])
	testutil.AssertEqualFloat(t, "engineVersion", 2, proj["engineVersion"])
	testutil.AssertEqual(t, "partitioning", "byStream", proj["partitioning"])

	if _, ok := proj["categories"]; !ok {
		t.Error("expected categories in JSON info")
	}
}

func TestJSONWriter_WriteError(t *testing.T) {
	var buf bytes.Buffer
	jw := newJSONWriter(&buf)

	jw.WriteError("5@order-1", "handler-error", "boom")

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	testutil.AssertEqual(t, "type", "error", line["type"])
	testutil.AssertEqual(t, "eventId", "5@order-1", line["eventId"])
	testutil.AssertEqual(t, "code", "handler-error", line["code"])
	testutil.AssertEqual(t, "description", "boom", line["description"])
}

func TestJSONWriter_WriteSummary_Partitioned(t *testing.T) {
	var buf bytes.Buffer
	jw := newJSONWriter(&buf)

	stats := engine.EventStats{Handled: 5, Skipped: 1}
	state := engine.StateSummary{
		Partitioned:   true,
		HasTransforms: true,
		HasBiState:    true,
		Partitions: map[string]engine.PartitionState{
			"order-1": {
				State:  json.RawMessage(`{"count":2}`),
				Result: json.RawMessage(`{"total":100}`),
			},
		},
		SharedState: json.RawMessage(`{"globalTotal":200}`),
	}
	jw.WriteSummary(stats, state)

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	testutil.AssertEqualFloat(t, "processed", 6, line["processed"])

	partitions, ok := line["partitions"].(map[string]any)
	if !ok {
		t.Fatal("expected partitions map")
	}
	if _, ok := partitions["order-1"]; !ok {
		t.Error("expected order-1 partition")
	}
	if _, ok := line["sharedState"]; !ok {
		t.Error("expected sharedState for biState projection")
	}

	if _, ok := line["state"]; ok {
		t.Error("partitioned summary should not have top-level state")
	}
}

func TestTextWriter_WriteSummary_WithTransforms(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)

	stats := engine.EventStats{Handled: 10}
	state := engine.StateSummary{
		State:         json.RawMessage(`{"count":10}`),
		Result:        json.RawMessage(`{"total":20}`),
		HasTransforms: true,
	}
	tw.WriteSummary(stats, state)

	out := buf.String()
	testutil.AssertContains(t, out, `State: {"count":10}`)
	testutil.AssertContains(t, out, `Result: {"total":20}`)
}

func TestTextWriter_WriteSummary_BiState(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)

	stats := engine.EventStats{Handled: 5}
	state := engine.StateSummary{
		Partitioned: true,
		HasBiState:  true,
		Partitions: map[string]engine.PartitionState{
			"p-1": {State: json.RawMessage(`{"x":1}`)},
		},
		SharedState: json.RawMessage(`{"global":true}`),
	}
	tw.WriteSummary(stats, state)

	out := buf.String()
	testutil.AssertContains(t, out, `Shared state: {"global":true}`)
	testutil.AssertContains(t, out, "p-1")
}

func TestTextWriter_SideEffects(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)

	ms := &mockSession{}
	tw.RegisterCallbacks(ms)

	ms.emitCb("notifications", "OrderReceived", `{"item":"Widget"}`, "", true, false)
	ms.logCb("hello from handler")
	ms.emitCb("shipped-orders", "", "", "", false, true)

	result := &gafferruntime.FeedResult{
		Status: "processed",
		State:  json.RawMessage(`{"count":1}`),
	}
	tw.WriteResult("1@order-1", result)

	out := buf.String()
	testutil.AssertContains(t, out, "emitted")
	testutil.AssertContains(t, out, "stream: notifications")
	testutil.AssertContains(t, out, "[log] hello from handler")
	testutil.AssertContains(t, out, "linked")
	testutil.AssertContains(t, out, "stream: shipped-orders")
	testutil.AssertContains(t, out, "processed")
}

type mockSession struct {
	emitCb gafferruntime.EmitCallback
	logCb  gafferruntime.LogCallback
}

func (m *mockSession) OnEmit(cb gafferruntime.EmitCallback) { m.emitCb = cb }
func (m *mockSession) OnLog(cb gafferruntime.LogCallback)   { m.logCb = cb }

func TestTextWriter_WriteError(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)

	tw.WriteError("1@order-1", "handler-error", "boom")

	out := buf.String()
	testutil.AssertContains(t, out, "handler-error")
	testutil.AssertContains(t, out, "boom")
}

func TestTextWriter_WriteSummary_WithErrors(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)

	stats := engine.EventStats{Handled: 5, Skipped: 1, Errors: 2}
	state := engine.StateSummary{}
	tw.WriteSummary(stats, state)

	out := buf.String()
	testutil.AssertContains(t, out, "8 events processed")
	testutil.AssertContains(t, out, "2 errors")
}

func TestDisplayJSON_String(t *testing.T) {
	raw := json.RawMessage(`"{\"cents\": 2999}"`)
	got := displayJSON(raw)
	testutil.AssertEqual(t, "displayJSON string", `{"cents": 2999}`, got)
}

func TestDisplayJSON_Object(t *testing.T) {
	raw := json.RawMessage(`{"cents":2999}`)
	got := displayJSON(raw)
	testutil.AssertEqual(t, "displayJSON object", `{"cents":2999}`, got)
}

func TestJSONWriter_WriteResult_SkippedWithLogs(t *testing.T) {
	var buf bytes.Buffer
	jw := newJSONWriter(&buf)

	result := &gafferruntime.FeedResult{
		Status:     "skipped",
		SkipReason: "unhandled",
		Logs:       []string{"debug info"},
	}
	jw.WriteResult("1@order-1", result)

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	testutil.AssertEqual(t, "status", "skipped", line["status"])
	if _, ok := line["logs"]; !ok {
		t.Error("expected logs on skipped result with logs")
	}
}

func TestJSONWriter_WriteResult_WithEmitted(t *testing.T) {
	var buf bytes.Buffer
	jw := newJSONWriter(&buf)

	data := `{"amount":50}`
	result := &gafferruntime.FeedResult{
		Status: "processed",
		State:  json.RawMessage(`{"count":1}`),
		Emitted: []gafferruntime.EmittedEvent{
			{StreamID: "out", EventType: "Created", IsJson: true, Data: &data},
		},
	}
	jw.WriteResult("1@s-1", result)

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	emitted, ok := line["emitted"].([]any)
	if !ok || len(emitted) != 1 {
		t.Fatal("expected 1 emitted event")
	}

	evt, ok := emitted[0].(map[string]any)
	if !ok {
		t.Fatal("expected emitted event object")
	}
	testutil.AssertEqual(t, "streamId", "out", evt["streamId"])
	testutil.AssertEqual(t, "eventType", "Created", evt["eventType"])
}

func TestToFatalError_InvalidProjection(t *testing.T) {
	err := &gafferruntime.InvalidProjectionError{
		Desc:     "Unexpected token",
		Location: &gafferruntime.JsLocation{Line: 5, Column: 12},
		Msg:      "Unexpected token",
	}

	fe := toFatalError(err, "/abs/path/projection.js")

	testutil.AssertEqual(t, "code", "invalid-projection", fe.Code)
	testutil.AssertEqual(t, "description", "Unexpected token", fe.Description)
	testutil.AssertEqual(t, "file", "/abs/path/projection.js", fe.File)
	if fe.Line == nil || *fe.Line != 5 {
		t.Errorf("expected line=5, got %v", fe.Line)
	}
	if fe.Column == nil || *fe.Column != 12 {
		t.Errorf("expected column=12, got %v", fe.Column)
	}
}

func TestToFatalError_HandlerError(t *testing.T) {
	err := &gafferruntime.ProjectionHandlerError{
		Desc:     "Cannot read property 'x' of undefined",
		JsStack:  "at line 10",
		Location: &gafferruntime.JsLocation{Line: 10, Column: 3},
		Event:    gafferruntime.EventContext{EventType: "ItemAdded", StreamID: "s-1", SequenceNumber: 5},
		Msg:      "handler threw",
	}

	fe := toFatalError(err, "/abs/path/projection.js")

	testutil.AssertEqual(t, "code", "handler-error", fe.Code)
	if fe.Line == nil || *fe.Line != 10 {
		t.Errorf("expected line=10, got %v", fe.Line)
	}
	testutil.AssertEqual(t, "jsStack", "at line 10", fe.JsStack)
	testutil.AssertEqual(t, "eventId", "5@s-1", fe.EventID)
}

func TestToFatalError_TransformError(t *testing.T) {
	err := &gafferruntime.ProjectionTransformError{
		Desc:     "transform threw",
		JsStack:  "at line 20",
		Location: &gafferruntime.JsLocation{Line: 20, Column: 5},
		Msg:      "transform threw",
	}

	fe := toFatalError(err, "/abs/path/projection.js")

	testutil.AssertEqual(t, "code", "projection-transform-error", fe.Code)
	if fe.Line == nil || *fe.Line != 20 {
		t.Errorf("expected line=20, got %v", fe.Line)
	}
	testutil.AssertEqual(t, "jsStack", "at line 20", fe.JsStack)
}

func TestToFatalError_ExecutionTimeout(t *testing.T) {
	err := &gafferruntime.ExecutionTimeoutError{
		Desc:      "execution timed out",
		ElapsedMs: 5100,
		AllowedMs: 5000,
		Event:     gafferruntime.EventContext{EventType: "Slow", StreamID: "s-2", SequenceNumber: 7},
		Msg:       "execution timed out",
	}

	fe := toFatalError(err, "/p.js")

	testutil.AssertEqual(t, "code", "execution-timeout", fe.Code)
	testutil.AssertContains(t, fe.Description, "elapsed 5100ms")
	testutil.AssertContains(t, fe.Description, "allowed 5000ms")
	testutil.AssertEqual(t, "eventId", "7@s-2", fe.EventID)
}

func TestToFatalError_MalformedEvent(t *testing.T) {
	err := &gafferruntime.MalformedEventError{
		Desc:  "JSON parse error",
		Event: gafferruntime.EventContext{EventType: "Bad", StreamID: "s-3", SequenceNumber: 2},
		Msg:   "JSON parse error",
	}

	fe := toFatalError(err, "/p.js")

	testutil.AssertEqual(t, "code", "malformed-event", fe.Code)
	testutil.AssertEqual(t, "eventId", "2@s-3", fe.EventID)
}

func TestToFatalError_FallbackDescription(t *testing.T) {
	err := &gafferruntime.InvalidProjectionError{
		Desc: "",
		Msg:  "raw message from runtime",
	}

	fe := toFatalError(err, "/p.js")

	testutil.AssertEqual(t, "description", "raw message from runtime", fe.Description)
}

func TestToFatalError_GenericError(t *testing.T) {
	err := errors.New("something exploded")

	fe := toFatalError(err, "/p.js")

	testutil.AssertEqual(t, "code", "unexpected-error", fe.Code)
	testutil.AssertEqual(t, "description", "something exploded", fe.Description)
	if fe.Line != nil {
		t.Errorf("expected nil line, got %v", *fe.Line)
	}
}

func TestJSONWriter_WriteFatalError(t *testing.T) {
	var buf bytes.Buffer
	jw := newJSONWriter(&buf)

	line := 5
	col := 12
	jw.WriteFatalError(fatalError{
		Code:        "invalid-projection",
		Description: "Unexpected token",
		File:        "/abs/path/projection.js",
		Line:        &line,
		Column:      &col,
	})

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	testutil.AssertEqual(t, "type", "fatal_error", got["type"])
	testutil.AssertEqual(t, "code", "invalid-projection", got["code"])
	testutil.AssertEqual(t, "description", "Unexpected token", got["description"])
	testutil.AssertEqual(t, "file", "/abs/path/projection.js", got["file"])
	testutil.AssertEqualFloat(t, "line", 5, got["line"])
	testutil.AssertEqualFloat(t, "column", 12, got["column"])
}

func TestJSONWriter_WriteFatalError_OmitsAbsentFields(t *testing.T) {
	var buf bytes.Buffer
	jw := newJSONWriter(&buf)

	jw.WriteFatalError(fatalError{
		Code:        "unexpected-error",
		Description: "boom",
	})

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if _, ok := got["line"]; ok {
		t.Error("line should be omitted when nil")
	}
	if _, ok := got["column"]; ok {
		t.Error("column should be omitted when nil")
	}
	if _, ok := got["file"]; ok {
		t.Error("file should be omitted when empty")
	}
	if _, ok := got["jsStack"]; ok {
		t.Error("jsStack should be omitted when empty")
	}
}

func TestTextWriter_WriteFatalError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	tw := newTextWriter(&stdout, &stderr)

	line := 5
	col := 12
	tw.WriteFatalError(fatalError{
		Code:        "invalid-projection",
		Description: "Unexpected token",
		File:        "/abs/path/projection.js",
		Line:        &line,
		Column:      &col,
	})

	if stdout.Len() != 0 {
		t.Errorf("stdout should be empty, got %q", stdout.String())
	}

	out := stderr.String()
	testutil.AssertContains(t, out, "invalid-projection")
	testutil.AssertContains(t, out, "Unexpected token")
	testutil.AssertContains(t, out, "/abs/path/projection.js:5:12")
}
