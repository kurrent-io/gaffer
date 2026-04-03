package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
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
	tw := newTextWriter(&buf)

	info := gafferruntime.QuerySources{
		AllStreams: true,
		ByStreams:  true,
		Events:     []string{"OrderPlaced", "OrderShipped"},
	}
	tw.WriteInfo("my-projection", info, "v2")

	out := buf.String()
	assertContains(t, out, "my-projection")
	assertContains(t, out, "Source: $all")
	assertContains(t, out, "Partitioning: per stream")
	assertContains(t, out, "Events: OrderPlaced, OrderShipped")
	assertContains(t, out, "Engine: v2")
}

func TestTextWriter_WriteInfo_BiStateAndProducesResults(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf)

	info := gafferruntime.QuerySources{
		AllStreams:      true,
		IsBiState:       true,
		ProducesResults: true,
	}
	tw.WriteInfo("bi-state-proj", info, "v2")

	out := buf.String()
	assertContains(t, out, "BiState: yes")
	assertContains(t, out, "Produces results: yes")
}

func TestTextWriter_WriteInfo_OmitsFalseFlags(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf)

	info := gafferruntime.QuerySources{
		AllStreams: true,
	}
	tw.WriteInfo("simple-proj", info, "v2")

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
	tw := newTextWriter(&buf)

	event := eventInfo{
		SequenceNumber: 1,
		StreamID:       "order-1",
		EventType:      "OrderPlaced",
		Data:           json.RawMessage(`{"amount":50}`),
		Metadata:       json.RawMessage(`{"corr":"abc"}`),
	}
	tw.WriteEvent(event)

	out := buf.String()
	assertContains(t, out, "1@order-1")
	assertContains(t, out, "type: OrderPlaced")
	assertContains(t, out, "data: {\"amount\":50}")
	assertContains(t, out, "metadata: {\"corr\":\"abc\"}")
}

func TestTextWriter_WriteResult_Processed(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf)

	result := &gafferruntime.FeedResult{
		Status:    "processed",
		Partition: "order-1",
		State:     json.RawMessage(`{"count":1}`),
	}
	tw.WriteResult("1@order-1", result)

	out := buf.String()
	assertContains(t, out, "partition: order-1\n")
	assertContains(t, out, `state: {"count":1}`)
}

func TestTextWriter_WriteResult_Skipped(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf)

	result := &gafferruntime.FeedResult{
		Status:     "skipped",
		SkipReason: "unhandled",
	}
	tw.WriteResult("1@order-1", result)

	out := buf.String()
	assertContains(t, out, "skipped\n")
	assertContains(t, out, "reason: unhandled")
}

func TestTextWriter_WriteSummary_Unpartitioned(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf)

	stats := engine.EventStats{Handled: 42, Skipped: 0}
	state := engine.StateSummary{
		State: json.RawMessage(`{"count":42}`),
	}
	tw.WriteSummary(stats, state)

	out := buf.String()
	assertContains(t, out, "42 events processed (42 handled, 0 skipped)")
	assertContains(t, out, `State: {"count":42}`)
}

func TestTextWriter_WriteSummary_Partitioned(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf)

	stats := engine.EventStats{Handled: 3, Skipped: 1}
	state := engine.StateSummary{
		Partitioned: true,
		Partitions: map[string]engine.PartitionState{
			"order-1": {State: json.RawMessage(`{"count":2}`)},
		},
	}
	tw.WriteSummary(stats, state)

	out := buf.String()
	assertContains(t, out, "4 events processed (3 handled, 1 skipped)")
	assertContains(t, out, "order-1")
	assertContains(t, out, `state: {"count":2}`)
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

	assertEqual(t, "type", "event", line["type"])
	assertEqual(t, "id", "1@order-1", line["id"])
	assertEqual(t, "eventType", "OrderPlaced", line["eventType"])
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

	assertEqual(t, "type", "result", line["type"])
	assertEqual(t, "status", "processed", line["status"])
	assertEqual(t, "partition", "order-1", line["partition"])

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

	assertEqual(t, "status", "skipped", line["status"])
	assertEqual(t, "reason", "unhandled", line["reason"])

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

	assertEqual(t, "type", "summary", line["type"])
	assertEqualFloat(t, "processed", 12, line["processed"])
	assertEqualFloat(t, "handled", 10, line["handled"])
	assertEqualFloat(t, "skipped", 2, line["skipped"])

	if _, ok := line["partitions"]; ok {
		t.Error("unpartitioned summary should not have partitions")
	}
}

func TestJSONWriter_WriteInfo(t *testing.T) {
	var buf bytes.Buffer
	jw := newJSONWriter(&buf)

	info := gafferruntime.QuerySources{
		Categories: []string{"order"},
		ByStreams:  true,
		Events:     []string{"OrderPlaced"},
	}
	jw.WriteInfo("my-projection", info, "v2")

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	assertEqual(t, "type", "info", line["type"])

	proj, ok := line["projection"].(map[string]any)
	if !ok {
		t.Fatal("expected projection object")
	}

	assertEqual(t, "name", "my-projection", proj["name"])
	assertEqual(t, "source", "category", proj["source"])
	assertEqual(t, "engine", "v2", proj["engine"])
	assertEqual(t, "partitioning", "per-stream", proj["partitioning"])

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

	assertEqual(t, "type", "error", line["type"])
	assertEqual(t, "eventId", "5@order-1", line["eventId"])
	assertEqual(t, "code", "handler-error", line["code"])
	assertEqual(t, "description", "boom", line["description"])
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

	assertEqualFloat(t, "processed", 6, line["processed"])

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
	tw := newTextWriter(&buf)

	stats := engine.EventStats{Handled: 10}
	state := engine.StateSummary{
		State:         json.RawMessage(`{"count":10}`),
		Result:        json.RawMessage(`{"total":20}`),
		HasTransforms: true,
	}
	tw.WriteSummary(stats, state)

	out := buf.String()
	assertContains(t, out, `State: {"count":10}`)
	assertContains(t, out, `Result: {"total":20}`)
}

func TestTextWriter_WriteSummary_BiState(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf)

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
	assertContains(t, out, `Shared state: {"global":true}`)
	assertContains(t, out, "p-1")
}

func TestTextWriter_SideEffects(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf)

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
	assertContains(t, out, "emitted")
	assertContains(t, out, "stream: notifications")
	assertContains(t, out, "[log] hello from handler")
	assertContains(t, out, "linked")
	assertContains(t, out, "stream: shipped-orders")
	assertContains(t, out, "processed")
}

type mockSession struct {
	emitCb gafferruntime.EmitCallback
	logCb  gafferruntime.LogCallback
}

func (m *mockSession) OnEmit(cb gafferruntime.EmitCallback) { m.emitCb = cb }
func (m *mockSession) OnLog(cb gafferruntime.LogCallback)   { m.logCb = cb }

func TestTextWriter_WriteError(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf)

	tw.WriteError("1@order-1", "handler-error", "boom")

	out := buf.String()
	assertContains(t, out, "handler-error")
	assertContains(t, out, "boom")
}

func TestTextWriter_WriteSummary_WithErrors(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf)

	stats := engine.EventStats{Handled: 5, Skipped: 1, Errors: 2}
	state := engine.StateSummary{}
	tw.WriteSummary(stats, state)

	out := buf.String()
	assertContains(t, out, "8 events processed")
	assertContains(t, out, "2 errors")
}

func TestDisplayJSON_String(t *testing.T) {
	raw := json.RawMessage(`"{\"cents\": 2999}"`)
	got := displayJSON(raw)
	assertEqual(t, "displayJSON string", `{"cents": 2999}`, got)
}

func TestDisplayJSON_Object(t *testing.T) {
	raw := json.RawMessage(`{"cents":2999}`)
	got := displayJSON(raw)
	assertEqual(t, "displayJSON object", `{"cents":2999}`, got)
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

	assertEqual(t, "status", "skipped", line["status"])
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
	assertEqual(t, "streamId", "out", evt["streamId"])
	assertEqual(t, "eventType", "Created", evt["eventType"])
}

func assertEqual[T comparable](t *testing.T, name string, want, got T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}

func assertEqualFloat(t *testing.T, name string, want float64, got any) {
	t.Helper()
	f, ok := got.(float64)
	if !ok {
		t.Errorf("%s: expected float64, got %T", name, got)
		return
	}
	if f != want {
		t.Errorf("%s: got %v, want %v", name, f, want)
	}
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected output to contain %q, got:\n%s", needle, haystack)
	}
}
