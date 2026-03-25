package dap

import (
	"bufio"
	"net"
	"testing"
	"time"

	godap "github.com/google/go-dap"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
)

// Full end-to-end test: DAP client connects, sets breakpoints,
// feeds events, hits breakpoint, inspects state, continues.
func TestIntegration_FullDebugFlow(t *testing.T) {
	opts := `{"debug":true}`
	source := "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function handler(s, e) {\ns.count++;\nreturn s;\n}\n})"
	session, err := gafferruntime.NewSession(source, &opts)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Destroy()

	adapter := NewDebugAdapter(session, "/tmp/test/projection.js")
	handler := adapter.Handler()
	srv, err := NewServer("127.0.0.1:0", handler)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Close() }()
	adapter.SetServer(srv)

	go func() { _ = srv.Serve() }()

	// Connect as a DAP client
	conn, err := net.Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)
	seq := 0

	send := func(msg godap.Message) {
		t.Helper()
		if err := godap.WriteProtocolMessage(conn, msg); err != nil {
			t.Fatalf("send failed: %v", err)
		}
	}

	recv := func() godap.Message {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		msg, err := godap.ReadProtocolMessage(reader)
		if err != nil {
			t.Fatalf("recv failed: %v", err)
		}
		return msg
	}

	nextSeq := func() int {
		seq++
		return seq
	}

	// 1. Initialize
	send(&godap.InitializeRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: nextSeq(), Type: "request"},
			Command:         "initialize",
		},
		Arguments: godap.InitializeRequestArguments{
			LinesStartAt1:   true,
			ColumnsStartAt1: true,
		},
	})

	msg := recv() // InitializeResponse
	if _, ok := msg.(*godap.InitializeResponse); !ok {
		t.Fatalf("expected InitializeResponse, got %T", msg)
	}
	msg = recv() // InitializedEvent
	if _, ok := msg.(*godap.InitializedEvent); !ok {
		t.Fatalf("expected InitializedEvent, got %T", msg)
	}

	// 2. Set breakpoints on line 4 (s.count++)
	send(&godap.SetBreakpointsRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: nextSeq(), Type: "request"},
			Command:         "setBreakpoints",
		},
		Arguments: godap.SetBreakpointsArguments{
			Source:      godap.Source{Path: "/tmp/test/projection.js"},
			Breakpoints: []godap.SourceBreakpoint{{Line: 4}},
		},
	})
	msg = recv()
	bpResp, ok := msg.(*godap.SetBreakpointsResponse)
	if !ok {
		t.Fatalf("expected SetBreakpointsResponse, got %T", msg)
	}
	if !bpResp.Body.Breakpoints[0].Verified {
		t.Fatal("breakpoint not verified")
	}

	// 3. Attach
	send(&godap.AttachRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: nextSeq(), Type: "request"},
			Command:         "attach",
		},
	})
	recv() // AttachResponse

	// 4. ConfigurationDone - triggers feeding
	send(&godap.ConfigurationDoneRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: nextSeq(), Type: "request"},
			Command:         "configurationDone",
		},
	})
	recv() // ConfigurationDoneResponse

	// 5. Feed an event in the background
	eventJSON := `{"eventType":"ItemAdded","streamId":"stream-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`
	feedDone := make(chan struct{})
	go func() {
		_, _ = adapter.FeedEvent(eventJSON)
		close(feedDone)
	}()

	// 6. Should get StoppedEvent
	msg = recv()
	stopped, ok := msg.(*godap.StoppedEvent)
	if !ok {
		t.Fatalf("expected StoppedEvent, got %T", msg)
	}
	if stopped.Body.Reason != "breakpoint" {
		t.Fatalf("expected breakpoint, got %s", stopped.Body.Reason)
	}
	t.Logf("stopped at breakpoint, threadId=%d", stopped.Body.ThreadId)

	// 7. Get stack trace
	send(&godap.StackTraceRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: nextSeq(), Type: "request"},
			Command:         "stackTrace",
		},
		Arguments: godap.StackTraceArguments{ThreadId: 1},
	})
	msg = recv()
	stResp, ok := msg.(*godap.StackTraceResponse)
	if !ok {
		t.Fatalf("expected StackTraceResponse, got %T", msg)
	}
	t.Logf("stack: %d frames, top=%s line=%d", len(stResp.Body.StackFrames), stResp.Body.StackFrames[0].Name, stResp.Body.StackFrames[0].Line)

	// 8. Get scopes
	send(&godap.ScopesRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: nextSeq(), Type: "request"},
			Command:         "scopes",
		},
		Arguments: godap.ScopesArguments{FrameId: stResp.Body.StackFrames[0].Id},
	})
	msg = recv()
	scResp, ok := msg.(*godap.ScopesResponse)
	if !ok {
		t.Fatalf("expected ScopesResponse, got %T", msg)
	}
	t.Logf("scopes: %d", len(scResp.Body.Scopes))

	// 9. Get variables
	send(&godap.VariablesRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: nextSeq(), Type: "request"},
			Command:         "variables",
		},
		Arguments: godap.VariablesArguments{VariablesReference: scResp.Body.Scopes[0].VariablesReference},
	})
	msg = recv()
	varResp, ok := msg.(*godap.VariablesResponse)
	if !ok {
		t.Fatalf("expected VariablesResponse, got %T", msg)
	}
	for _, v := range varResp.Body.Variables {
		t.Logf("  %s = %s (%s)", v.Name, v.Value, v.Type)
	}

	// 10. Continue
	send(&godap.ContinueRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: nextSeq(), Type: "request"},
			Command:         "continue",
		},
		Arguments: godap.ContinueArguments{ThreadId: 1},
	})
	recv() // ContinueResponse
	recv() // ContinuedEvent

	// 11. Feed should complete
	select {
	case <-feedDone:
		t.Log("feed completed successfully")
	case <-time.After(5 * time.Second):
		t.Fatal("feed did not complete")
	}
}
