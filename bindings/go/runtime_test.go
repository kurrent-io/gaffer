package gafferruntime

import (
	"encoding/json"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func mustCreateSession(t *testing.T, source string) *Session {
	t.Helper()
	session, err := NewSession(source, &v2Opts)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	t.Cleanup(func() { session.Destroy() })
	return session
}

func mustFeed(t *testing.T, session *Session, eventJSON string) *FeedResult {
	t.Helper()
	result, err := session.Feed(eventJSON)
	if err != nil {
		t.Fatalf("Feed failed: %v", err)
	}
	return result
}

func mustGetState(t *testing.T, session *Session, partition *string) string {
	t.Helper()
	state := session.GetState(partition)
	if state == nil {
		t.Fatal("GetState returned nil")
		return ""
	}
	return *state
}

func TestSessionCreateAndDestroy(t *testing.T) {
	session, err := NewSession(`
		fromAll().when({
			$init() { return {}; },
			Ping(s, e) { return s; }
		})
	`, &v2Opts)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	session.Destroy()
}

func TestCreateWithInvalidJS(t *testing.T) {
	_, err := NewSession("this is not valid {{{{", &v2Opts)
	if err == nil {
		t.Fatal("expected error for invalid JS")
	}
	var projErr *InvalidProjectionError
	if !errors.As(err, &projErr) {
		t.Fatalf("expected InvalidProjectionError, got %T", err)
	}
	if projErr.Location == nil {
		t.Fatal("expected location on parse error")
	}
}

func TestFeedAndGetState(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			$init() { return { count: 0 }; },
			ItemAdded(s, e) { s.count++; return s; }
		})
	`)

	mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"cart-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)
	mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"cart-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)
	mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"cart-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	state := mustGetState(t, session, nil)
	if state != `{"count":3}` {
		t.Fatalf("expected {\"count\":3}, got %s", state)
	}
}

// TestSessionHandleIsNotPointer is the deterministic guard for the
// integer-handle-in-pointer-field bug. The runtime returns small integer
// handles (1, 2, 3, ...), so Session.handle must not be a pointer type: a
// pointer-typed field puts those integers in the GC's stack maps, which aborts
// the process ("invalid pointer found on stack"). Unlike the concurrent-GC
// test below, this fails immediately and regardless of scheduling or -race.
func TestSessionHandleIsNotPointer(t *testing.T) {
	f, ok := reflect.TypeOf(Session{}).FieldByName("handle")
	if !ok {
		t.Fatal("Session has no handle field")
	}
	if f.Type.Kind() == reflect.Pointer {
		t.Fatalf("Session.handle must not be a pointer type, got %s", f.Type)
	}
}

// TestSessionHandleSurvivesConcurrentGC exercises the FFI under GC pressure:
// it feeds concurrently across sessions while a background goroutine churns
// runtime.GC(), so a stack scan can coincide with a live handle mid-Feed
// (consumeError -> json.Unmarshal) or mid-callback (the handle flows back
// through user_data into the emit trampoline). With the handle stored as a
// pointer this could trip the GC's invalidptr abort; uintptr storage and an
// integer-typed user_data keep it off the stack maps. This is a probabilistic
// behavioural check - TestSessionHandleIsNotPointer is the deterministic guard
// against the field type regressing.
func TestSessionHandleSurvivesConcurrentGC(t *testing.T) {
	const (
		sessions = 8
		rounds   = 20
		event    = `{"eventType":"ItemAdded","streamId":"cart-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`
	)

	ss := make([]*Session, sessions)
	for i := range ss {
		ss[i] = mustCreateSession(t, `
			fromAll().when({
				$init() { return { count: 0 }; },
				ItemAdded(s, e) { s.count++; emit("counter-out", "Counted", { n: s.count }); return s; }
			})
		`)
		ss[i].OnEmit(func(_, _, _, _ string, _, _ bool) {})
	}

	stop := make(chan struct{})
	gcDone := make(chan struct{})
	go func() {
		defer close(gcDone)
		for {
			select {
			case <-stop:
				return
			default:
				runtime.GC()
			}
		}
	}()

	var wg sync.WaitGroup
	for _, s := range ss {
		wg.Add(1)
		go func(s *Session) {
			defer wg.Done()
			for range rounds {
				if _, err := s.Feed(event); err != nil {
					t.Errorf("Feed failed: %v", err)
					return
				}
			}
		}(s)
	}
	wg.Wait()
	close(stop)
	<-gcDone

	for _, s := range ss {
		if got := mustGetState(t, s, nil); got != `{"count":20}` {
			t.Fatalf("expected {\"count\":20}, got %s", got)
		}
	}
}

// TestCallbackPanicIsRethrownAndSessionRecovers checks that a panic from a user
// callback is captured (not unwound through the runtime's .NET frames) and
// re-raised on the Go side with its original value once Feed returns, and that
// the session stays usable afterwards - proving the panic didn't abandon the
// handler mid-flight.
func TestCallbackPanicIsRethrownAndSessionRecovers(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			$init() { return { count: 0 }; },
			Tick(s, e) { s.count++; emit("out", "Ticked", { n: s.count }); return s; }
		})
	`)

	shouldPanic := true
	session.OnEmit(func(_, _, _, _ string, _, _ bool) {
		if shouldPanic {
			panic("boom from callback")
		}
	})

	const event = `{"eventType":"Tick","streamId":"clock-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`

	recovered := func() (r any) {
		defer func() { r = recover() }()
		_, _ = session.Feed(event)
		return nil
	}()
	if recovered == nil {
		t.Fatal("expected Feed to re-raise the callback panic, but it returned normally")
	}
	if msg, ok := recovered.(string); !ok || msg != "boom from callback" {
		t.Fatalf("expected re-raised panic %q, got %v (%T)", "boom from callback", recovered, recovered)
	}

	// Session still works: the runtime completed the handler and its cleanup.
	shouldPanic = false
	mustFeed(t, session, event)
	if got := mustGetState(t, session, nil); got != `{"count":2}` {
		t.Fatalf("expected {\"count\":2} after two feeds, got %s", got)
	}
}

func TestEventDataAccessible(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			$init() { return { total: 0 }; },
			Deposited(s, e) { s.total += e.data.amount; return s; }
		})
	`)

	mustFeed(t, session, `{"eventType":"Deposited","streamId":"acc-1","sequenceNumber":0,"data":"{\"amount\":50}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)
	mustFeed(t, session, `{"eventType":"Deposited","streamId":"acc-1","sequenceNumber":0,"data":"{\"amount\":30}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	state := mustGetState(t, session, nil)
	if state != `{"total":80}` {
		t.Fatalf("expected {\"total\":80}, got %s", state)
	}
}

func TestGetSources(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().foreachStream().when({
			$init() { return {}; },
			Ping(s, e) { return s; }
		})
	`)

	sources := session.GetSources()
	if !sources.ByStreams {
		t.Fatal("expected ByStreams to be true")
	}
	if len(sources.Diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %v", sources.Diagnostics)
	}
}

func TestGetSourcesReportsLinkStreamToDeprecation(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			$any: function (s, e) {
				linkStreamTo("archive-" + e.streamId, e.streamId);
				return s;
			}
		})
	`)

	sources := session.GetSources()
	if len(sources.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %v", len(sources.Diagnostics), sources.Diagnostics)
	}
	d := sources.Diagnostics[0]
	if d.Code != "usage.linkStreamTo.deprecated" {
		t.Errorf("expected code usage.linkStreamTo.deprecated, got %q", d.Code)
	}
	if d.Severity != DiagnosticSeverityInformation {
		t.Errorf("expected severity Information (3), got %d", d.Severity)
	}
	if !strings.Contains(d.Message, "linkStreamTo") {
		t.Errorf("expected message to mention linkStreamTo, got %q", d.Message)
	}
	if d.Range == nil {
		t.Fatal("expected range to be set")
	}
	// linkStreamTo is on the 4th line of the multiline source above
	// (line 1 = "", line 2 = fromAll().when(...), line 3 = $any:...,
	// line 4 = linkStreamTo(...)). 1-based.
	if d.Range.Start.Line != 4 {
		t.Errorf("expected start line 4, got %d", d.Range.Start.Line)
	}
	if d.Range.Start.Column < 1 {
		t.Errorf("expected 1-based start column, got %d", d.Range.Start.Column)
	}
	if d.Range.End.Column-d.Range.Start.Column != len("linkStreamTo") {
		t.Errorf("expected end-start to span %d chars, got %d",
			len("linkStreamTo"), d.Range.End.Column-d.Range.Start.Column)
	}
}

// TestProjectionInfo_DecodesDiagnosticsWireFormat pins the JSON shape
// independently of the runtime so a tag drift (e.g. "code" -> "id") is
// caught even when the runtime hasn't been rebuilt yet.
func TestProjectionInfo_DecodesDiagnosticsWireFormat(t *testing.T) {
	cases := []struct {
		name string
		json string
		want int
	}{
		{"null", `{"allStreams":true,"diagnostics":null}`, 0},
		{"empty", `{"allStreams":true,"diagnostics":[]}`, 0},
		{
			"populated",
			`{"allStreams":true,"diagnostics":[{` +
				`"code":"usage.linkStreamTo.deprecated",` +
				`"message":"linkStreamTo is undocumented",` +
				`"severity":2,` +
				`"range":{"start":{"line":3,"column":5},"end":{"line":3,"column":17}}` +
				`}]}`,
			1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var info ProjectionInfo
			if err := json.Unmarshal([]byte(tc.json), &info); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if len(info.Diagnostics) != tc.want {
				t.Fatalf("want %d diagnostics, got %d", tc.want, len(info.Diagnostics))
			}
			if tc.want == 0 {
				return
			}
			d := info.Diagnostics[0]
			if d.Code != "usage.linkStreamTo.deprecated" {
				t.Errorf("code: %q", d.Code)
			}
			if d.Severity != DiagnosticSeverityWarning {
				t.Errorf("severity: %d", d.Severity)
			}
			if d.Range == nil ||
				d.Range.Start.Line != 3 || d.Range.Start.Column != 5 ||
				d.Range.End.Line != 3 || d.Range.End.Column != 17 {
				t.Errorf("range: %+v", d.Range)
			}
		})
	}
}

func TestForeachStreamPartitioning(t *testing.T) {
	session := mustCreateSession(t, `
		fromCategory("cart").foreachStream().when({
			$init() { return { items: 0 }; },
			ItemAdded(s, e) { s.items++; return s; }
		})
	`)

	mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"cart-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)
	mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"cart-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)
	mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"cart-2","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	p1 := "cart-1"
	state1 := mustGetState(t, session, &p1)
	if state1 != `{"items":2}` {
		t.Fatalf("cart-1: expected {\"items\":2}, got %s", state1)
	}

	p2 := "cart-2"
	state2 := mustGetState(t, session, &p2)
	if state2 != `{"items":1}` {
		t.Fatalf("cart-2: expected {\"items\":1}, got %s", state2)
	}
}

func TestSetAndRestoreState(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			$init() { return { count: 0 }; },
			Ping(s, e) { s.count++; return s; }
		})
	`)

	session.SetState(nil, `{"count":10}`)
	mustFeed(t, session, `{"eventType":"Ping","streamId":"s-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	state := mustGetState(t, session, nil)
	if state != `{"count":11}` {
		t.Fatalf("expected {\"count\":11}, got %s", state)
	}
}

func TestFeedError(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			$init() { return {}; },
			Bad(s, e) { throw "boom"; }
		})
	`)

	_, err := session.Feed(`{"eventType":"Bad","streamId":"s-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)
	if err == nil {
		t.Fatal("expected error")
	}
	var handlerErr *ProjectionHandlerError
	if !errors.As(err, &handlerErr) {
		t.Fatalf("expected ProjectionHandlerError, got %T", err)
	}
	if handlerErr.Desc != "boom" {
		t.Fatalf("expected description 'boom', got %s", handlerErr.Desc)
	}
	if handlerErr.Event.EventType != "Bad" {
		t.Fatalf("expected eventType 'Bad', got %s", handlerErr.Event.EventType)
	}
}

func TestCreateWithOptions(t *testing.T) {
	opts := `{"engineVersion":2,"compilationTimeoutMs":10000}`
	session, err := NewSession(`
		fromAll().when({
			$init() { return {}; },
			Ping(s, e) { return s; }
		})
	`, &opts)
	if err != nil {
		t.Fatalf("NewSession with options failed: %v", err)
	}
	session.Destroy()
}

func TestUnknownPartitionReturnsNil(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().foreachStream().when({
			$init() { return {}; },
			Ping(s, e) { return s; }
		})
	`)

	p := "nonexistent"
	state := session.GetState(&p)
	if state != nil {
		t.Fatalf("expected nil, got %s", *state)
	}
}

func TestDoubleDestroyIsSafe(t *testing.T) {
	session, err := NewSession(`
		fromAll().when({
			$init() { return {}; },
			Ping(s, e) { return s; }
		})
	`, &v2Opts)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	session.Destroy()
	session.Destroy()
}

func TestOnEmitCallback(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			$init() { return {}; },
			OrderPlaced(s, e) {
				emit("notifications", "OrderNotification", { orderId: e.data.orderId });
				return s;
			}
		})
	`)

	var emitted []struct{ streamID, eventType, data string }
	session.OnEmit(func(streamID, eventType, data, _ string, _, _ bool) {
		emitted = append(emitted, struct{ streamID, eventType, data string }{streamID, eventType, data})
	})

	mustFeed(t, session, `{"eventType":"OrderPlaced","streamId":"order-1","sequenceNumber":0,"data":"{\"orderId\":\"ABC\"}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	if len(emitted) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(emitted))
	}
	if emitted[0].streamID != "notifications" {
		t.Fatalf("expected stream 'notifications', got %s", emitted[0].streamID)
	}
	if emitted[0].eventType != "OrderNotification" {
		t.Fatalf("expected type 'OrderNotification', got %s", emitted[0].eventType)
	}
	if !strings.Contains(emitted[0].data, "ABC") {
		t.Fatalf("expected data to contain 'ABC', got %s", emitted[0].data)
	}
}

func TestOnLogCallback(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			TestEvent(s, e) {
				log("hello from projection");
				return s;
			}
		})
	`)

	var logs []string
	session.OnLog(func(message string) {
		logs = append(logs, message)
	})

	mustFeed(t, session, `{"eventType":"TestEvent","streamId":"s-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	if logs[0] != "hello from projection" {
		t.Fatalf("expected 'hello from projection', got %s", logs[0])
	}
}

func TestOnStateChangedCallback(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			$init() { return { count: 0 }; },
			ItemAdded(s, e) { s.count++; return s; }
		})
	`)

	var changes []string
	session.OnStateChanged(func(_ string, stateJSON string) {
		changes = append(changes, stateJSON)
	})

	mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"s-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)
	mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"s-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	if len(changes) != 2 {
		t.Fatalf("expected 2 state changes, got %d", len(changes))
	}
	if !strings.Contains(changes[0], `"count":1`) {
		t.Fatalf("expected count:1, got %s", changes[0])
	}
	if !strings.Contains(changes[1], `"count":2`) {
		t.Fatalf("expected count:2, got %s", changes[1])
	}
}

func TestBiStateSharedState(t *testing.T) {
	session := mustCreateSession(t, `
		options({ biState: true });
		fromAll().when({
			$init() { return { count: 0 }; },
			$initShared() { return { total: 0 }; },
			Added(s, e) {
				s[0].count++;
				s[1].total += e.data.amount;
				return s;
			}
		})
	`)

	mustFeed(t, session, `{"eventType":"Added","streamId":"s-1","sequenceNumber":0,"data":"{\"amount\":10}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)
	mustFeed(t, session, `{"eventType":"Added","streamId":"s-1","sequenceNumber":0,"data":"{\"amount\":20}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	state := mustGetState(t, session, nil)
	if !strings.Contains(state, `"count":2`) {
		t.Fatalf("expected count:2 in state, got %s", state)
	}

	shared := session.GetSharedState()
	if shared == nil {
		t.Fatal("expected shared state")
		return
	}
	if !strings.Contains(*shared, `"total":30`) {
		t.Fatalf("expected total:30 in shared state, got %s", *shared)
	}
}

func TestGetResultWithTransformBy(t *testing.T) {
	// V1 only - V2 doesn't iterate transforms; result == post-handler state.
	session, err := NewSession(`
		fromAll().when({
			$init() { return { count: 0 }; },
			Ping(s, e) { s.count++; return s; }
		}).transformBy(function(s) {
			return { total: s.count * 2 };
		}).outputState()
	`, &v1Opts)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	t.Cleanup(func() { session.Destroy() })

	mustFeed(t, session, `{"eventType":"Ping","streamId":"s-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	result, err := session.GetResult(nil)
	if err != nil {
		t.Fatalf("GetResult failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
		return
	}
	if !strings.Contains(*result, `"total":2`) {
		t.Fatalf("expected total:2, got %s", *result)
	}
}

func TestGetPartitionKey(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().partitionBy(function(e) {
			return e.data.region;
		}).when({
			$init() { return {}; },
			Event(s, e) { return s; }
		})
	`)

	key := session.GetPartitionKey(`{"eventType":"Event","streamId":"s-1","sequenceNumber":0,"data":"{\"region\":\"eu\"}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)
	if key == nil {
		t.Fatal("expected partition key")
		return
	}
	if *key != "eu" {
		t.Fatalf("expected 'eu', got %s", *key)
	}
}

func TestFeedResultProcessed(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			$init() { return { count: 0 }; },
			ItemAdded(s, e) { s.count++; return s; }
		})
	`)

	result := mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"cart-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	if result.Status != "processed" {
		t.Fatalf("expected status 'processed', got %q", result.Status)
	}
	if len(result.State) == 0 || string(result.State) == "null" {
		t.Fatal("expected non-null state")
	}
	var state map[string]interface{}
	if err := json.Unmarshal(result.State, &state); err != nil {
		t.Fatalf("failed to parse state: %v", err)
	}
	if state["count"] != float64(1) {
		t.Fatalf("expected count 1, got %v", state["count"])
	}
}

func TestFeedResultSkipped(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			$init() { return {}; },
			Handled(s, e) { return s; }
		})
	`)

	result := mustFeed(t, session, `{"eventType":"Unhandled","streamId":"s-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	if result.Status != "skipped" {
		t.Fatalf("expected status 'skipped', got %q", result.Status)
	}
	if result.SkipReason == "" {
		t.Fatal("expected non-empty skip reason")
	}
}

func TestFeedResultEmittedEvents(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			$init() { return {}; },
			OrderPlaced(s, e) {
				emit("notifications", "OrderNotification", { orderId: e.data.orderId });
				return s;
			}
		})
	`)

	result := mustFeed(t, session, `{"eventType":"OrderPlaced","streamId":"order-1","sequenceNumber":0,"data":"{\"orderId\":\"ABC\"}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	if result.Status != "processed" {
		t.Fatalf("expected status 'processed', got %q", result.Status)
	}
	if len(result.Emitted) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(result.Emitted))
	}
	if result.Emitted[0].StreamID != "notifications" {
		t.Fatalf("expected stream 'notifications', got %q", result.Emitted[0].StreamID)
	}
	if result.Emitted[0].EventType != "OrderNotification" {
		t.Fatalf("expected type 'OrderNotification', got %q", result.Emitted[0].EventType)
	}
	if result.Emitted[0].Data == nil || !strings.Contains(*result.Emitted[0].Data, "ABC") {
		t.Fatalf("expected data containing 'ABC', got %v", result.Emitted[0].Data)
	}
}

func TestFeedResultPartition(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().foreachStream().when({
			$init() { return { count: 0 }; },
			Ping(s, e) { s.count++; return s; }
		})
	`)

	result := mustFeed(t, session, `{"eventType":"Ping","streamId":"order-42","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	if result.Status != "processed" {
		t.Fatalf("expected status 'processed', got %q", result.Status)
	}
	if result.Partition != "order-42" {
		t.Fatalf("expected partition 'order-42', got %q", result.Partition)
	}
}

// TestIncludeShape_PopulatesProjectionInfoShape exercises the
// includeShape FFI option end-to-end: caller asks for shape, the
// runtime walker runs alongside the diagnostic pass, and the
// returned ProjectionInfo carries the result.
func TestIncludeShape_PopulatesProjectionInfoShape(t *testing.T) {
	opts := `{"engineVersion":2,"includeShape":true}`
	session, err := NewSession(`
		fromAll().when({
			$init: function() { return { n: 0 }; },
			$any: function(s, e) { s.n++; return s; },
			Order: function(s, e) { return s; },
			Refund: function(s, e) { return s; },
		}).outputState();
	`, &opts)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(func() { session.Destroy() })

	info := session.GetSources()
	if info.Shape == nil {
		t.Fatal("Shape was nil; expected populated when includeShape=true")
	}
	if !info.Shape.Parsable {
		t.Error("Parsable = false; expected true for a valid projection")
	}
	if info.Shape.FileSize == 0 {
		t.Error("FileSize = 0; expected non-zero raw byte count")
	}
	if !info.Shape.Handlers.Any {
		t.Error("Handlers.Any = false; expected true")
	}
	if !info.Shape.Handlers.Init {
		t.Error("Handlers.Init = false; expected true")
	}
	if info.Shape.Handlers.DistinctEventNames != 2 {
		t.Errorf("DistinctEventNames = %d, want 2 (Order + Refund)",
			info.Shape.Handlers.DistinctEventNames)
	}
	if info.Shape.BuiltinCounts.FromAll == nil || *info.Shape.BuiltinCounts.FromAll != 1 {
		t.Errorf("FromAll = %v, want &1", info.Shape.BuiltinCounts.FromAll)
	}
	if info.Shape.BuiltinCounts.When == nil || *info.Shape.BuiltinCounts.When != 1 {
		t.Errorf("When = %v, want &1", info.Shape.BuiltinCounts.When)
	}
	if info.Shape.BuiltinCounts.OutputState == nil || *info.Shape.BuiltinCounts.OutputState != 1 {
		t.Errorf("OutputState = %v, want &1", info.Shape.BuiltinCounts.OutputState)
	}
}

// TestIncludeShape_DefaultsFalseShapeIsNil pins that omitting the
// includeShape option leaves Shape nil. LSP and any other consumer
// that doesn't ask for shape pays nothing for it.
func TestIncludeShape_DefaultsFalseShapeIsNil(t *testing.T) {
	session := mustCreateSession(t, "fromAll();")
	info := session.GetSources()
	if info.Shape != nil {
		t.Errorf("Shape = %+v, want nil (includeShape not set)", info.Shape)
	}
}
