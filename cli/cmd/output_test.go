package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

// stubProjection builds a synthetic *engine.Projection for output writer
// tests that previously took (name, engineVersion, quirksVersion) directly.
func stubProjection(name string, engineVersion int, quirksVersion string) *engine.Projection {
	return &engine.Projection{
		Def:           &config.Projection{Name: name},
		EngineVersion: engineVersion,
		QuirksVersion: quirksVersion,
	}
}

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
	tw.WriteInfo(stubProjection("my-projection", 2, ""), info)

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
	tw.WriteInfo(stubProjection("bi-state-proj", 2, ""), info)

	out := buf.String()
	testutil.AssertContains(t, out, "BiState: yes")
	testutil.AssertContains(t, out, "Produces results: yes")
}

func TestTextWriter_WriteInfo_RendersDiagnostics(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)

	info := gafferruntime.ProjectionInfo{
		AllStreams: true,
		Diagnostics: []gafferruntime.Diagnostic{{
			Code:     "usage.linkStreamTo.deprecated",
			Message:  "linkStreamTo is undocumented in KurrentDB and may be removed in a future version.",
			Severity: gafferruntime.DiagnosticSeverityWarning,
			Range: &gafferruntime.SourceRange{
				Start: gafferruntime.SourcePosition{Line: 3, Column: 5},
				End:   gafferruntime.SourcePosition{Line: 3, Column: 17},
			},
		}},
	}
	tw.WriteInfo(stubProjection("p", 2, ""), info)

	out := buf.String()
	testutil.AssertContains(t, out, "[warning]")
	testutil.AssertContains(t, out, "usage.linkStreamTo.deprecated")
	testutil.AssertContains(t, out, "line 3, col 5")
	testutil.AssertContains(t, out, "linkStreamTo is undocumented")
}

func TestTextWriter_WriteInfo_DiagnosticWithoutRange(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)

	info := gafferruntime.ProjectionInfo{
		AllStreams: true,
		Diagnostics: []gafferruntime.Diagnostic{{
			Code:     "usage.something.deprecated",
			Message:  "no location available",
			Severity: gafferruntime.DiagnosticSeverityWarning,
		}},
	}
	tw.WriteInfo(stubProjection("p", 2, ""), info)

	out := buf.String()
	testutil.AssertContains(t, out, "usage.something.deprecated")
	testutil.AssertContains(t, out, "no location available")
	if strings.Contains(out, "(line ") {
		t.Error("expected no line/column info when range is nil")
	}
}

func TestTextWriter_WriteInfo_QuirksVersion_Set(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)

	tw.WriteInfo(stubProjection("p", 2, "26.1.0"), gafferruntime.ProjectionInfo{AllStreams: true})

	out := buf.String()
	testutil.AssertContains(t, out, "Quirks: 26.1.0")
}

func TestTextWriter_WriteInfo_QuirksVersion_Unversioned(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)

	tw.WriteInfo(stubProjection("p", 2, ""), gafferruntime.ProjectionInfo{AllStreams: true})

	out := buf.String()
	testutil.AssertContains(t, out, "unversioned")
	testutil.AssertContains(t, out, "matching all KurrentDB quirks")
}

func TestJSONWriter_WriteInfo_QuirksVersion_Set(t *testing.T) {
	var buf bytes.Buffer
	jw := newJSONWriter(&buf)

	jw.WriteInfo(stubProjection("p", 2, "26.1.0"), gafferruntime.ProjectionInfo{AllStreams: true})

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	proj := line["projection"].(map[string]any)
	testutil.AssertEqual(t, "quirksVersion", "26.1.0", proj["quirksVersion"])
}

func TestJSONWriter_WriteInfo_QuirksVersion_NullWhenUnset(t *testing.T) {
	var buf bytes.Buffer
	jw := newJSONWriter(&buf)

	jw.WriteInfo(stubProjection("p", 2, ""), gafferruntime.ProjectionInfo{AllStreams: true})

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	proj := line["projection"].(map[string]any)
	v, ok := proj["quirksVersion"]
	if !ok {
		t.Fatal("expected quirksVersion to be present (as null) when empty")
	}
	if v != nil {
		t.Errorf("expected quirksVersion null, got %v (%T)", v, v)
	}
}

func TestTextWriter_WriteFatalError_RendersCompatBlock(t *testing.T) {
	var stdout, stderr bytes.Buffer
	tw := newTextWriter(&stdout, &stderr)

	// A real fatal quirk error: the runtime enriches it with the catalogue
	// description (and fixedIn, nil today) alongside the code.
	tw.WriteFatalError(fatalError{
		Code:              "handler-error",
		Description:       "Argument is not an object",
		CompatCode:        "quirk.event.bodyCast",
		CompatDescription: "Accessing event.body throws on a non-object body.",
	})

	out := stderr.String()
	testutil.AssertContains(t, out, "Compat:")
	testutil.AssertContains(t, out, "quirk.event.bodyCast")
	testutil.AssertContains(t, out, "Accessing event.body throws on a non-object body.")
	// With no compatFixedIn on the error, the rendering
	// shows "Current KurrentDB behaviour" rather than "Fixed in ...".
	testutil.AssertContains(t, out, "Current KurrentDB behaviour")
}

func TestTextWriter_WriteCompatBlock_RendersFixedInWhenSet(t *testing.T) {
	// The "Fixed in KurrentDB X" branch activates when the runtime sets
	// compatFixedIn on the error. The description + fixedIn ride the error
	// payload, no registry round-trip.
	var buf bytes.Buffer
	tw := newTextWriter(&buf, &buf)

	tw.writeCompatBlock(&buf, fatalError{
		CompatCode:        "quirk.event.bodyCast",
		CompatDescription: "Accessing event.body throws on non-object bodies.",
		CompatFixedIn:     "26.1.1",
	})

	out := buf.String()
	testutil.AssertContains(t, out, "Compat:")
	testutil.AssertContains(t, out, "quirk.event.bodyCast")
	testutil.AssertContains(t, out, "Accessing event.body throws on non-object bodies.")
	testutil.AssertContains(t, out, "Fixed in KurrentDB 26.1.1.")
	if strings.Contains(out, "Current KurrentDB behaviour") {
		t.Error("expected Fixed-in branch, got Current-behaviour line")
	}
}

func TestToFatalError_PropagatesCompatCodeFromInvalidArgument(t *testing.T) {
	err := &gafferruntime.InvalidArgumentError{
		Desc:       "bad input",
		Field:      "quirksVersion",
		CompatCode: "quirk.test.synthetic",
		Msg:        "bad input",
	}
	fe := toFatalError(err, "/p.js")
	testutil.AssertEqual(t, "compatCode", "quirk.test.synthetic", fe.CompatCode)
}

func TestTextWriter_WriteFatalError_OmitsCompatBlockWhenAbsent(t *testing.T) {
	var stdout, stderr bytes.Buffer
	tw := newTextWriter(&stdout, &stderr)

	tw.WriteFatalError(fatalError{
		Code:        "invalid-projection",
		Description: "Unexpected token",
	})

	out := stderr.String()
	if strings.Contains(out, "Compat:") {
		t.Errorf("expected no Compat block when CompatCode is empty, got:\n%s", out)
	}
}

func TestJSONWriter_WriteFatalError_IncludesCompatCode(t *testing.T) {
	var buf bytes.Buffer
	jw := newJSONWriter(&buf)

	jw.WriteFatalError(fatalError{
		Code:        "handler-error",
		Description: "boom",
		CompatCode:  "quirk.event.bodyCast",
	})

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	testutil.AssertEqual(t, "compatCode", "quirk.event.bodyCast", got["compatCode"])
}

func TestJSONWriter_WriteFatalError_OmitsCompatCodeWhenEmpty(t *testing.T) {
	var buf bytes.Buffer
	jw := newJSONWriter(&buf)

	jw.WriteFatalError(fatalError{Code: "handler-error", Description: "boom"})

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := got["compatCode"]; ok {
		t.Error("expected compatCode to be omitted when empty")
	}
}

func TestToFatalError_PropagatesCompatCode(t *testing.T) {
	err := &gafferruntime.ProjectionHandlerError{
		Desc:       "boom",
		Event:      gafferruntime.EventContext{StreamID: "s-1", SequenceNumber: 1},
		CompatCode: "quirk.event.bodyCast",
		Msg:        "boom",
	}
	fe := toFatalError(err, "/p.js")
	testutil.AssertEqual(t, "compatCode", "quirk.event.bodyCast", fe.CompatCode)
}

func TestTextWriter_WriteInfo_OmitsFalseFlags(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)

	info := gafferruntime.ProjectionInfo{
		AllStreams: true,
	}
	tw.WriteInfo(stubProjection("simple-proj", 2, ""), info)

	out := buf.String()
	if strings.Contains(out, "BiState") {
		t.Error("should not show BiState when false")
	}
	if strings.Contains(out, "Produces results") {
		t.Error("should not show Produces results when false")
	}
}

func TestTextWriter_WriteEvent_DeferredUntilResult(t *testing.T) {
	// WriteEvent buffers; the actual print only happens on a
	// non-skipped WriteResult / WriteError. Verifies the contract
	// that lets us drop skipped events transparently from text output.
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
	if buf.Len() != 0 {
		t.Fatalf("expected nothing written before the result, got %q", buf.String())
	}

	tw.WriteResult("1@order-1", &gafferruntime.FeedResult{Status: "processed"})

	out := buf.String()
	testutil.AssertContains(t, out, "1@order-1")
	testutil.AssertContains(t, out, "type: OrderPlaced")
	testutil.AssertContains(t, out, "data: {\"amount\":50}")
	testutil.AssertContains(t, out, "metadata: {\"corr\":\"abc\"}")
}

func TestTextWriter_WriteResult_Processed(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)

	tw.WriteEvent(eventInfo{SequenceNumber: 1, StreamID: "order-1", EventType: "OrderPlaced"})
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

func TestTextWriter_OnDiagnostic_RendersInline(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)
	ms := &mockSession{}
	tw.RegisterCallbacks(ms)

	// Quirks stream during the event, in the ├ flow before the result.
	tw.WriteEvent(eventInfo{SequenceNumber: 1, StreamID: "s-1", EventType: "SetName"})
	ms.diagCb(gafferruntime.Diagnostic{
		Code:     "quirk.log.multiParam",
		Message:  "log() with multiple arguments produces unexpected output.",
		Severity: gafferruntime.DiagnosticSeverityWarning,
	})
	tw.WriteResult("1@s-1", &gafferruntime.FeedResult{
		Status: "processed",
		State:  json.RawMessage(`"\"alice\""`),
	})

	out := buf.String()
	testutil.AssertContains(t, out, "[warning] quirk.log.multiParam")
	testutil.AssertContains(t, out, "log() with multiple arguments")
}

func TestTextWriter_WriteSummary_QuirksBreakdown(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)
	ms := &mockSession{}
	tw.RegisterCallbacks(ms)

	// Two distinct runtime quirks stream during the run; the summary lists
	// each once, no per-code count, with a header total.
	ms.diagCb(gafferruntime.Diagnostic{Code: "quirk.log.multiParam", Severity: gafferruntime.DiagnosticSeverityWarning})
	ms.diagCb(gafferruntime.Diagnostic{Code: "quirk.serialize.nonFinite", Severity: gafferruntime.DiagnosticSeverityWarning})
	tw.WriteSummary(engine.EventStats{Handled: 3}, engine.StateSummary{})

	out := buf.String()
	testutil.AssertContains(t, out, "2 quirks encountered")
	testutil.AssertContains(t, out, "quirk.log.multiParam")
	testutil.AssertContains(t, out, "quirk.serialize.nonFinite")
	// Linked once per summary, pointing at the diagnostics reference.
	testutil.AssertContains(t, out, "See https://gaffer.kurrent.io/reference/diagnostics/")
}

func TestTextWriter_WriteSummary_MergesCompileTimeQuirks(t *testing.T) {
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)
	ms := &mockSession{}
	tw.RegisterCallbacks(ms)

	// A compile-time compat quirk from the info header folds into the summary
	// alongside runtime quirks (deduped; deprecations excluded).
	tw.WriteInfo(stubProjection("p", 2, ""), gafferruntime.ProjectionInfo{
		AllStreams: true,
		Diagnostics: []gafferruntime.Diagnostic{
			{Code: "quirk.log.multiParam", Message: "m", Severity: gafferruntime.DiagnosticSeverityWarning},
			{Code: "usage.linkStreamTo.deprecated", Message: "d", Severity: gafferruntime.DiagnosticSeverityWarning},
		},
	})
	ms.diagCb(gafferruntime.Diagnostic{Code: "quirk.serialize.nonFinite", Severity: gafferruntime.DiagnosticSeverityError})
	tw.WriteSummary(engine.EventStats{Handled: 1}, engine.StateSummary{})

	out := buf.String()
	testutil.AssertContains(t, out, "2 quirks encountered")
	testutil.AssertContains(t, out, "quirk.log.multiParam")
	testutil.AssertContains(t, out, "quirk.serialize.nonFinite")
	// Linked once per summary, pointing at the diagnostics reference.
	testutil.AssertContains(t, out, "See https://gaffer.kurrent.io/reference/diagnostics/")
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
	testutil.AssertContains(t, out, "42 events processed")
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
	testutil.AssertContains(t, out, "3 events processed")
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

func TestJSONWriter_WriteResult_Diagnostics(t *testing.T) {
	var buf bytes.Buffer
	jw := newJSONWriter(&buf)

	jw.WriteResult("1@s-1", &gafferruntime.FeedResult{
		Status: "processed",
		Diagnostics: []gafferruntime.Diagnostic{{
			Code:     "quirk.log.multiParam",
			Message:  "log() with multiple arguments produces unexpected output.",
			Severity: gafferruntime.DiagnosticSeverityWarning,
		}},
	})

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	diags, ok := line["diagnostics"].([]any)
	if !ok || len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %v", line["diagnostics"])
	}
	d := diags[0].(map[string]any)
	testutil.AssertEqual(t, "code", "quirk.log.multiParam", d["code"])
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
	jw.WriteInfo(stubProjection("my-projection", 2, ""), info)

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

func TestJSONWriter_WriteInfo_IncludesDiagnostics(t *testing.T) {
	var buf bytes.Buffer
	jw := newJSONWriter(&buf)

	info := gafferruntime.ProjectionInfo{
		AllStreams: true,
		Diagnostics: []gafferruntime.Diagnostic{{
			Code:     "usage.linkStreamTo.deprecated",
			Message:  "linkStreamTo is undocumented",
			Severity: gafferruntime.DiagnosticSeverityWarning,
			Range: &gafferruntime.SourceRange{
				Start: gafferruntime.SourcePosition{Line: 3, Column: 5},
				End:   gafferruntime.SourcePosition{Line: 3, Column: 17},
			},
		}},
	}
	jw.WriteInfo(stubProjection("p", 2, ""), info)

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	proj, ok := line["projection"].(map[string]any)
	if !ok {
		t.Fatal("expected projection object")
	}
	diags, ok := proj["diagnostics"].([]any)
	if !ok || len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %v", proj["diagnostics"])
	}
	d, ok := diags[0].(map[string]any)
	if !ok {
		t.Fatal("expected diagnostic object")
	}
	testutil.AssertEqual(t, "code", "usage.linkStreamTo.deprecated", d["code"])
	testutil.AssertEqualFloat(t, "severity", 2, d["severity"])
}

func TestJSONWriter_WriteInfo_OmitsEmptyDiagnostics(t *testing.T) {
	var buf bytes.Buffer
	jw := newJSONWriter(&buf)

	jw.WriteInfo(stubProjection("p", 2, ""), gafferruntime.ProjectionInfo{AllStreams: true})

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	proj := line["projection"].(map[string]any)
	if _, ok := proj["diagnostics"]; ok {
		t.Error("expected diagnostics to be omitted when empty")
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
	diagCb gafferruntime.DiagnosticCallback
}

func (m *mockSession) OnEmit(cb gafferruntime.EmitCallback) { m.emitCb = cb }
func (m *mockSession) OnLog(cb gafferruntime.LogCallback)   { m.logCb = cb }
func (m *mockSession) OnDiagnostic(cb gafferruntime.DiagnosticCallback) {
	m.diagCb = cb
}

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
	testutil.AssertContains(t, out, "5 events processed")
	testutil.AssertContains(t, out, "2 errors")
}

func TestTextWriter_WriteSummary_FixtureMode_SkipBreakdown(t *testing.T) {
	// Fixture mode renders a breakdown by reason so the user can see
	// why specific curated events didn't run.
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)
	tw.showSkipped = true

	stats := engine.EventStats{
		Handled: 5,
		Skipped: 4,
		SkippedByReason: map[string]int{
			"unhandled":    3,
			"no-partition": 1,
		},
	}
	tw.WriteSummary(stats, engine.StateSummary{})

	out := buf.String()
	testutil.AssertContains(t, out, "5 events processed")
	testutil.AssertContains(t, out, "4 events skipped")
	testutil.AssertContains(t, out, "3 no handler for this event type")
	testutil.AssertContains(t, out, "1 partitionBy returned null")
}

func TestTextWriter_WriteResult_FixtureMode_RendersSkip(t *testing.T) {
	// Fixture mode shows the skip row + reason. Live mode (covered
	// elsewhere) drops both.
	var buf bytes.Buffer
	tw := newTextWriter(&buf, nil)
	tw.showSkipped = true

	tw.WriteEvent(eventInfo{SequenceNumber: 1, StreamID: "deletes-1", EventType: "$streamDeleted"})
	tw.WriteResult("1@deletes-1", &gafferruntime.FeedResult{
		Status:     "skipped",
		SkipReason: "no-delete-handler",
	})

	out := buf.String()
	testutil.AssertContains(t, out, "1@deletes-1")
	testutil.AssertContains(t, out, "skipped")
	testutil.AssertContains(t, out, "reason: no-delete-handler")
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
