package dap

import (
	"bufio"
	"encoding/json"
	"net"
	"testing"
	"time"

	godap "github.com/google/go-dap"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/history"
)

const testDebugOpts = `{"engineVersion":2,"debug":true}`

const testFeedEvent = `{"eventType":"ItemAdded","streamId":"stream-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`

func mustSetupDebugSession(t *testing.T) (*DebugAdapter, *engine.Runner, net.Conn, *bufio.Reader) {
	t.Helper()
	return mustSetupDebugSessionSource(t,
		"fromAll().when({\n$init() { return { count: 0 }; },\nItemAdded(s, e) {\ns.count++;\nreturn s;\n}\n})")
}

func mustSetupDebugSessionSource(t *testing.T, source string) (*DebugAdapter, *engine.Runner, net.Conn, *bufio.Reader) {
	t.Helper()
	opts := testDebugOpts
	session, err := gafferruntime.NewSession(source, &opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Destroy() })

	adapter := NewDebugAdapter(session, "/tmp/test/projection.js", "/tmp/test")
	runner := engine.NewRunner(engine.RunnerConfig{
		Feed:    engine.FeedFn(session.Feed),
		Session: session,
		Info:    session.GetSources(),
		Writer:  adapter.EventWriter(),
		Debug: &engine.DebugConfig{
			Session: session,
			Info:    session.GetSources(),
			OnBreak: adapter.HandleBreak,
		},
	})
	adapter.SetRunner(runner)

	handler := adapter.Handler()
	srv, err := NewServer("127.0.0.1:0", handler)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	adapter.SetServer(srv)

	connCh := make(chan net.Conn, 1)
	go func() {
		conn, err := net.Dial("tcp", srv.Addr().String())
		if err != nil {
			return
		}
		connCh <- conn
	}()
	go func() { _ = srv.Serve() }()

	var conn net.Conn
	select {
	case conn = <-connCh:
		t.Cleanup(func() { _ = conn.Close() })
	case <-time.After(5 * time.Second):
		t.Fatal("timed out connecting")
	}

	reader := bufio.NewReader(conn)

	// Initialize
	sendRequest(t, conn, &godap.InitializeRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 1, Type: "request"},
			Command:         "initialize",
		},
		Arguments: godap.InitializeRequestArguments{
			LinesStartAt1:   true,
			ColumnsStartAt1: true,
		},
	})
	readMessage(t, conn, reader) // InitializeResponse
	readMessage(t, conn, reader) // InitializedEvent

	return adapter, runner, conn, reader
}

func TestAdapter_SetBreakpointsAndPause(t *testing.T) {
	_, runner, conn, reader := mustSetupDebugSession(t)

	// Set breakpoint on line 4
	sendRequest(t, conn, &godap.SetBreakpointsRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 2, Type: "request"},
			Command:         "setBreakpoints",
		},
		Arguments: godap.SetBreakpointsArguments{
			Source:      godap.Source{Path: "/tmp/test/projection.js"},
			Breakpoints: []godap.SourceBreakpoint{{Line: 4}},
		},
	})

	msg := readMessage(t, conn, reader)
	bpResp, ok := msg.(*godap.SetBreakpointsResponse)
	if !ok {
		t.Fatalf("expected SetBreakpointsResponse, got %T", msg)
	}
	if len(bpResp.Body.Breakpoints) != 1 {
		t.Fatalf("expected 1 breakpoint, got %d", len(bpResp.Body.Breakpoints))
	}
	if !bpResp.Body.Breakpoints[0].Verified {
		t.Fatal("expected breakpoint to be verified")
	}

	// ConfigurationDone
	sendRequest(t, conn, &godap.ConfigurationDoneRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 3, Type: "request"},
			Command:         "configurationDone",
		},
	})
	readMessage(t, conn, reader) // ConfigurationDoneResponse

	// Feed an event in the background
	feedDone := make(chan struct{})
	go func() {
		runner.ProcessOne(testFeedEvent)
		close(feedDone)
	}()

	// Should get a stopped event
	msg = readMessage(t, conn, reader)
	stopped, ok := msg.(*godap.StoppedEvent)
	if !ok {
		t.Fatalf("expected StoppedEvent, got %T", msg)
	}
	if stopped.Body.Reason != "breakpoint" {
		t.Fatalf("expected breakpoint reason, got %s", stopped.Body.Reason)
	}

	// Get stack trace
	sendRequest(t, conn, &godap.StackTraceRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 4, Type: "request"},
			Command:         "stackTrace",
		},
		Arguments: godap.StackTraceArguments{ThreadId: 1},
	})
	msg = readMessage(t, conn, reader)
	stResp, ok := msg.(*godap.StackTraceResponse)
	if !ok {
		t.Fatalf("expected StackTraceResponse, got %T", msg)
	}
	if len(stResp.Body.StackFrames) < 1 {
		t.Fatal("expected at least 1 stack frame")
	}
	if stResp.Body.StackFrames[0].Name != "ItemAdded" {
		t.Fatalf("expected ItemAdded, got %s", stResp.Body.StackFrames[0].Name)
	}

	// Get scopes
	sendRequest(t, conn, &godap.ScopesRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 5, Type: "request"},
			Command:         "scopes",
		},
		Arguments: godap.ScopesArguments{FrameId: 0},
	})
	msg = readMessage(t, conn, reader)
	scResp, ok := msg.(*godap.ScopesResponse)
	if !ok {
		t.Fatalf("expected ScopesResponse, got %T", msg)
	}
	if len(scResp.Body.Scopes) < 1 {
		t.Fatal("expected at least 1 scope")
	}

	// Get variables
	sendRequest(t, conn, &godap.VariablesRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 6, Type: "request"},
			Command:         "variables",
		},
		Arguments: godap.VariablesArguments{VariablesReference: scResp.Body.Scopes[0].VariablesReference},
	})
	msg = readMessage(t, conn, reader)
	varResp, ok := msg.(*godap.VariablesResponse)
	if !ok {
		t.Fatalf("expected VariablesResponse, got %T", msg)
	}
	if len(varResp.Body.Variables) < 1 {
		t.Fatal("expected at least 1 variable")
	}

	// Continue
	sendRequest(t, conn, &godap.ContinueRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 7, Type: "request"},
			Command:         "continue",
		},
		Arguments: godap.ContinueArguments{ThreadId: 1},
	})
	readMessage(t, conn, reader) // ContinueResponse
	readMessage(t, conn, reader) // ContinuedEvent

	// Feed should complete
	select {
	case <-feedDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for feed to complete")
	}
}

func TestAdapter_PathMapping_MismatchedRoots(t *testing.T) {
	a := &DebugAdapter{
		sourcePath: "/workspaces/gaffer/demo/projections/counter.js",
		remoteRoot: "/workspaces/gaffer/demo",
		localRoot:  "/home/user/dev/gaffer/demo",
	}

	local := a.toLocal("/workspaces/gaffer/demo/projections/counter.js")
	if local != "/home/user/dev/gaffer/demo/projections/counter.js" {
		t.Errorf("toLocal: got %s", local)
	}

	remote := a.toRemote("/home/user/dev/gaffer/demo/projections/counter.js")
	if remote != "/workspaces/gaffer/demo/projections/counter.js" {
		t.Errorf("toRemote: got %s", remote)
	}
}

func TestAdapter_PathMapping_MatchingRoots(t *testing.T) {
	a := &DebugAdapter{
		sourcePath: "/home/user/proj/projection.js",
		remoteRoot: "/home/user/proj",
		localRoot:  "/home/user/proj",
	}

	local := a.toLocal("/home/user/proj/projection.js")
	if local != "/home/user/proj/projection.js" {
		t.Errorf("toLocal should be no-op, got %s", local)
	}

	remote := a.toRemote("/home/user/proj/projection.js")
	if remote != "/home/user/proj/projection.js" {
		t.Errorf("toRemote should be no-op, got %s", remote)
	}
}

func TestAdapter_PathMapping_NoLocalRoot(t *testing.T) {
	a := &DebugAdapter{
		sourcePath: "/workspaces/gaffer/projection.js",
		remoteRoot: "/workspaces/gaffer",
	}

	local := a.toLocal("/workspaces/gaffer/projection.js")
	if local != "/workspaces/gaffer/projection.js" {
		t.Errorf("toLocal should be no-op without localRoot, got %s", local)
	}
}

func TestAdapter_PathMapping_BreakpointMatching(t *testing.T) {
	a := &DebugAdapter{
		sourcePath: "/workspaces/gaffer/demo/projections/counter.js",
		remoteRoot: "/workspaces/gaffer/demo",
		localRoot:  "/home/user/dev/gaffer/demo",
	}

	editorPath := "/home/user/dev/gaffer/demo/projections/counter.js"
	remotePath := a.toRemote(editorPath)
	if remotePath != a.sourcePath {
		t.Errorf("editor path should map to sourcePath: got %s, want %s", remotePath, a.sourcePath)
	}

	wrongFile := "/home/user/dev/gaffer/demo/projections/other.js"
	remotePath = a.toRemote(wrongFile)
	if remotePath == a.sourcePath {
		t.Error("wrong file should not map to sourcePath")
	}
}

func TestAdapter_PathMapping_TrailingSlash(t *testing.T) {
	a := &DebugAdapter{
		sourcePath: "/workspaces/gaffer/projection.js",
		remoteRoot: "/workspaces/gaffer/",
		localRoot:  "/home/user/gaffer/",
	}

	local := a.toLocal("/workspaces/gaffer/projection.js")
	if local != "/home/user/gaffer/projection.js" {
		t.Errorf("toLocal with trailing slashes: got %s", local)
	}
}

func TestAdapter_PathMapping_PartialPrefixNoMatch(t *testing.T) {
	a := &DebugAdapter{
		sourcePath: "/workspaces/gaffer/projection.js",
		remoteRoot: "/workspaces/gaffer",
		localRoot:  "/home/user/gaffer",
	}

	result := a.toLocal("/workspaces/gaffer2/other.js")
	if result != "/workspaces/gaffer2/other.js" {
		t.Errorf("partial prefix should not match: got %s", result)
	}
}

func mustSetupDebugSessionWithHistory(t *testing.T) (*DebugAdapter, *engine.Runner, net.Conn, *bufio.Reader) {
	t.Helper()
	opts := testDebugOpts
	source := "fromAll().when({\n$init() { return { count: 0 }; },\nItemAdded(s, e) {\ns.count++;\nreturn s;\n}\n})"
	session, err := gafferruntime.NewSession(source, &opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Destroy() })

	store, err := history.New()
	if err != nil {
		t.Fatal(err)
	}

	adapter := NewDebugAdapter(session, "/tmp/test/projection.js", "/tmp/test")
	runner := engine.NewRunner(engine.RunnerConfig{
		Feed:    engine.FeedFn(session.Feed),
		Session: session,
		Info:    session.GetSources(),
		Writer:  adapter.EventWriter(),
		History: store,
		Debug: &engine.DebugConfig{
			Session: session,
			Info:    session.GetSources(),
			OnBreak: adapter.HandleBreak,
		},
	})
	adapter.SetRunner(runner)

	handler := adapter.Handler()
	srv, err := NewServer("127.0.0.1:0", handler)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	adapter.SetServer(srv)

	connCh := make(chan net.Conn, 1)
	go func() {
		conn, err := net.Dial("tcp", srv.Addr().String())
		if err != nil {
			return
		}
		connCh <- conn
	}()
	go func() { _ = srv.Serve() }()

	var conn net.Conn
	select {
	case conn = <-connCh:
		t.Cleanup(func() { _ = conn.Close() })
	case <-time.After(5 * time.Second):
		t.Fatal("timed out connecting")
	}

	reader := bufio.NewReader(conn)

	sendRequest(t, conn, &godap.InitializeRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 1, Type: "request"},
			Command:         "initialize",
		},
		Arguments: godap.InitializeRequestArguments{
			LinesStartAt1:   true,
			ColumnsStartAt1: true,
		},
	})
	readMessage(t, conn, reader) // InitializeResponse
	readMessage(t, conn, reader) // InitializedEvent

	return adapter, runner, conn, reader
}

func TestAdapter_SendTerminated(t *testing.T) {
	adapter, runner, conn, reader := mustSetupDebugSession(t)

	sendRequest(t, conn, &godap.ConfigurationDoneRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 2, Type: "request"},
			Command:         "configurationDone",
		},
	})
	readMessage(t, conn, reader) // ConfigurationDoneResponse

	// Process an event so there's real state to snapshot.
	runner.ProcessOne(testFeedEvent)

	adapter.SendTerminated()

	// Order: gaffer/finalState (snapshot) -> TerminatedEvent -> ExitedEvent.
	// The snapshot pre-populates the editor's per-partition cache so the
	// post-mortem view doesn't fall back to "(not loaded during session)".
	body := readCustomEvent(t, conn, reader, "gaffer/finalState")
	if body["state"] == nil {
		t.Fatalf("expected state body in final snapshot, got %+v", body)
	}

	msg := readMessage(t, conn, reader)
	if _, ok := msg.(*godap.TerminatedEvent); !ok {
		t.Fatalf("expected TerminatedEvent, got %T", msg)
	}

	msg = readMessage(t, conn, reader)
	if _, ok := msg.(*godap.ExitedEvent); !ok {
		t.Fatalf("expected ExitedEvent, got %T", msg)
	}
}

func TestAdapter_HandleRestart_BlocksUntilAcked(t *testing.T) {
	adapter, _, conn, reader := mustSetupDebugSession(t)

	// Drain the dev.go side: read off the restart request, recreate
	// state, and ack. In production this is the main loop's job.
	go func() {
		<-adapter.RestartRequested()
		adapter.ResetForRestart()
		adapter.AckRestart()
	}()

	sendRequest(t, conn, &godap.RestartRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 2, Type: "request"},
			Command:         "restart",
		},
	})

	// Expected order: RestartResponse, then a fresh InitializedEvent
	// (so VS Code resends breakpoints + configurationDone).
	if _, ok := readMessage(t, conn, reader).(*godap.RestartResponse); !ok {
		t.Fatal("expected RestartResponse")
	}
	if _, ok := readMessage(t, conn, reader).(*godap.InitializedEvent); !ok {
		t.Fatal("expected InitializedEvent after restart")
	}

	// Adapter Ready() must be re-armed after ResetForRestart so the
	// next configurationDone unblocks the main loop.
	select {
	case <-adapter.Ready():
		t.Fatal("Ready should not fire before configurationDone")
	default:
	}
	sendRequest(t, conn, &godap.ConfigurationDoneRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 3, Type: "request"},
			Command:         "configurationDone",
		},
	})
	readMessage(t, conn, reader) // ConfigurationDoneResponse
	select {
	case <-adapter.Ready():
	case <-time.After(time.Second):
		t.Fatal("Ready did not fire after post-restart configurationDone")
	}
}

func TestAdapter_HandleDisconnect_EmitsFinalStateBeforeResponse(t *testing.T) {
	adapter, runner, conn, reader := mustSetupDebugSession(t)

	sendRequest(t, conn, &godap.ConfigurationDoneRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 2, Type: "request"},
			Command:         "configurationDone",
		},
	})
	readMessage(t, conn, reader) // ConfigurationDoneResponse

	runner.ProcessOne(testFeedEvent)

	sendRequest(t, conn, &godap.DisconnectRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 3, Type: "request"},
			Command:         "disconnect",
		},
	})

	// finalState must precede the DisconnectResponse: VS Code closes
	// the socket once it sees the response, so a snapshot queued after
	// would never reach the editor.
	body := readCustomEvent(t, conn, reader, "gaffer/finalState")
	if body["state"] == nil {
		t.Fatalf("expected state body in final snapshot, got %+v", body)
	}

	msg := readMessage(t, conn, reader)
	if _, ok := msg.(*godap.DisconnectResponse); !ok {
		t.Fatalf("expected DisconnectResponse, got %T", msg)
	}

	// Subsequent SendTerminated should NOT emit a second finalState
	// (sync.Once dedup); only the Terminated/Exited pair.
	adapter.SendTerminated()
	msg = readMessage(t, conn, reader)
	if _, ok := msg.(*godap.TerminatedEvent); !ok {
		t.Fatalf("expected TerminatedEvent after disconnect, got %T", msg)
	}
	msg = readMessage(t, conn, reader)
	if _, ok := msg.(*godap.ExitedEvent); !ok {
		t.Fatalf("expected ExitedEvent after disconnect, got %T", msg)
	}
}

// feedAndContinue sets a breakpoint, feeds an event, waits for the stopped event,
// then continues execution. Returns the next available seq number.
func feedAndContinue(t *testing.T, runner *engine.Runner, conn net.Conn, reader *bufio.Reader, startSeq int) int {
	t.Helper()
	seq := startSeq

	sendRequest(t, conn, &godap.SetBreakpointsRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: seq, Type: "request"},
			Command:         "setBreakpoints",
		},
		Arguments: godap.SetBreakpointsArguments{
			Source:      godap.Source{Path: "/tmp/test/projection.js"},
			Breakpoints: []godap.SourceBreakpoint{{Line: 4}},
		},
	})
	seq++
	readMessage(t, conn, reader) // SetBreakpointsResponse

	sendRequest(t, conn, &godap.ConfigurationDoneRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: seq, Type: "request"},
			Command:         "configurationDone",
		},
	})
	seq++
	readMessage(t, conn, reader) // ConfigurationDoneResponse

	feedDone := make(chan struct{})
	go func() {
		runner.ProcessOne(testFeedEvent)
		close(feedDone)
	}()

	msg := readMessage(t, conn, reader)
	if _, ok := msg.(*godap.StoppedEvent); !ok {
		t.Fatalf("expected StoppedEvent, got %T", msg)
	}

	sendRequest(t, conn, &godap.ContinueRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: seq, Type: "request"},
			Command:         "continue",
		},
		Arguments: godap.ContinueArguments{ThreadId: 1},
	})
	seq++
	readMessage(t, conn, reader) // ContinueResponse
	readMessage(t, conn, reader) // ContinuedEvent

	select {
	case <-feedDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for feed to complete")
	}

	return seq
}

func TestAdapter_GafferGoto(t *testing.T) {
	_, runner, conn, reader := mustSetupDebugSessionWithHistory(t)

	seq := feedAndContinue(t, runner, conn, reader, 2)

	sendRequest(t, conn, &GafferGotoRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: seq, Type: "request"},
			Command:         "gaffer/goto",
		},
		Arguments: GafferGotoArguments{Step: 1},
	})

	msg := readMessage(t, conn, reader)
	resp, ok := msg.(*GafferGotoResponse)
	if !ok {
		t.Fatalf("expected GafferGotoResponse, got %T", msg)
	}
	if !resp.Success {
		t.Fatal("expected success response")
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		t.Fatalf("failed to unmarshal body: %v", err)
	}
	if _, ok := body["step"]; !ok {
		t.Fatal("expected step in response body")
	}
	if _, ok := body["event"]; !ok {
		t.Fatal("expected event in response body")
	}
	if _, ok := body["result"]; !ok {
		t.Fatal("expected result in response body")
	}
}

func TestAdapter_GafferGoto_InvalidStep(t *testing.T) {
	_, runner, conn, reader := mustSetupDebugSessionWithHistory(t)

	seq := feedAndContinue(t, runner, conn, reader, 2)

	sendRequest(t, conn, &GafferGotoRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: seq, Type: "request"},
			Command:         "gaffer/goto",
		},
		Arguments: GafferGotoArguments{Step: 999},
	})

	msg := readMessage(t, conn, reader)
	resp, ok := msg.(*godap.ErrorResponse)
	if !ok {
		t.Fatalf("expected ErrorResponse, got %T", msg)
	}
	if resp.Success {
		t.Fatal("expected error response")
	}
	if resp.Message != "step not found" {
		t.Fatalf("expected 'step not found', got %q", resp.Message)
	}
}

func TestAdapter_GafferGoto_NoHistory(t *testing.T) {
	_, _, conn, reader := mustSetupDebugSession(t)

	sendRequest(t, conn, &godap.ConfigurationDoneRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 2, Type: "request"},
			Command:         "configurationDone",
		},
	})
	readMessage(t, conn, reader) // ConfigurationDoneResponse

	sendRequest(t, conn, &GafferGotoRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 3, Type: "request"},
			Command:         "gaffer/goto",
		},
		Arguments: GafferGotoArguments{Step: 1},
	})

	msg := readMessage(t, conn, reader)
	resp, ok := msg.(*godap.ErrorResponse)
	if !ok {
		t.Fatalf("expected ErrorResponse, got %T", msg)
	}
	if resp.Success {
		t.Fatal("expected error response")
	}
}

func TestAdapter_GafferTimeline(t *testing.T) {
	_, runner, conn, reader := mustSetupDebugSessionWithHistory(t)

	seq := feedAndContinue(t, runner, conn, reader, 2)

	sendRequest(t, conn, &GafferTimelineRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: seq, Type: "request"},
			Command:         "gaffer/timeline",
		},
		Arguments: GafferTimelineArguments{From: 0, To: 10},
	})

	msg := readMessage(t, conn, reader)
	resp, ok := msg.(*GafferTimelineResponse)
	if !ok {
		t.Fatalf("expected GafferTimelineResponse, got %T", msg)
	}
	if !resp.Success {
		t.Fatal("expected success response")
	}

	var entries []json.RawMessage
	if err := json.Unmarshal(resp.Body, &entries); err != nil {
		t.Fatalf("failed to unmarshal timeline body: %v", err)
	}
	if len(entries) < 1 {
		t.Fatal("expected at least 1 timeline entry")
	}
}

func TestAdapter_GafferTimeline_NoHistory(t *testing.T) {
	_, _, conn, reader := mustSetupDebugSession(t)

	sendRequest(t, conn, &godap.ConfigurationDoneRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 2, Type: "request"},
			Command:         "configurationDone",
		},
	})
	readMessage(t, conn, reader) // ConfigurationDoneResponse

	sendRequest(t, conn, &GafferTimelineRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 3, Type: "request"},
			Command:         "gaffer/timeline",
		},
		Arguments: GafferTimelineArguments{From: 0, To: 10},
	})

	msg := readMessage(t, conn, reader)
	resp, ok := msg.(*godap.ErrorResponse)
	if !ok {
		t.Fatalf("expected ErrorResponse, got %T", msg)
	}
	if resp.Success {
		t.Fatal("expected error response")
	}
}

func TestAdapter_GafferPartitionState(t *testing.T) {
	_, runner, conn, reader := mustSetupDebugSessionWithHistory(t)

	seq := feedAndContinue(t, runner, conn, reader, 2)

	sendRequest(t, conn, &GafferPartitionStateRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: seq, Type: "request"},
			Command:         "gaffer/partitionState",
		},
		Arguments: GafferPartitionStateArguments{Partition: ""},
	})

	msg := readMessage(t, conn, reader)
	resp, ok := msg.(*GafferPartitionStateResponse)
	if !ok {
		t.Fatalf("expected GafferPartitionStateResponse, got %T", msg)
	}
	if !resp.Success {
		t.Fatal("expected success response")
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		t.Fatalf("failed to unmarshal body: %v", err)
	}
	if _, ok := body["partition"]; !ok {
		t.Fatal("expected partition in response body")
	}
	if _, ok := body["state"]; !ok {
		t.Fatal("expected state in response body")
	}
}

func TestAdapter_GafferPartitionState_UnknownPartition(t *testing.T) {
	_, _, conn, reader := mustSetupDebugSession(t)

	sendRequest(t, conn, &godap.ConfigurationDoneRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 2, Type: "request"},
			Command:         "configurationDone",
		},
	})
	readMessage(t, conn, reader) // ConfigurationDoneResponse

	sendRequest(t, conn, &GafferPartitionStateRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 3, Type: "request"},
			Command:         "gaffer/partitionState",
		},
		Arguments: GafferPartitionStateArguments{Partition: "nonexistent"},
	})

	msg := readMessage(t, conn, reader)
	resp, ok := msg.(*GafferPartitionStateResponse)
	if !ok {
		t.Fatalf("expected GafferPartitionStateResponse, got %T", msg)
	}
	if !resp.Success {
		t.Fatal("expected success response even for unknown partition")
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		t.Fatalf("failed to unmarshal body: %v", err)
	}
	if _, ok := body["partition"]; !ok {
		t.Fatal("expected partition in response body")
	}
	if _, ok := body["state"]; ok {
		t.Fatal("expected no state for unknown partition")
	}
}

func TestAdapter_StartPausedIfNoBreakpoints_PausesWhenNoBreakpointsSet(t *testing.T) {
	adapter, runner, conn, reader := mustSetupDebugSession(t)
	adapter.SetStartPausedIfNoBreakpoints(true)

	sendRequest(t, conn, &godap.ConfigurationDoneRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 2, Type: "request"},
			Command:         "configurationDone",
		},
	})
	readMessage(t, conn, reader) // ConfigurationDoneResponse

	feedDone := make(chan struct{})
	go func() {
		runner.ProcessOne(testFeedEvent)
		close(feedDone)
	}()

	msg := readMessage(t, conn, reader)
	stopped, ok := msg.(*godap.StoppedEvent)
	if !ok {
		t.Fatalf("expected StoppedEvent at entry, got %T", msg)
	}
	if stopped.Body.Reason != "entry" {
		t.Fatalf("expected entry reason, got %s", stopped.Body.Reason)
	}

	sendRequest(t, conn, &godap.ContinueRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 3, Type: "request"},
			Command:         "continue",
		},
		Arguments: godap.ContinueArguments{ThreadId: 1},
	})
	readMessage(t, conn, reader) // ContinueResponse
	readMessage(t, conn, reader) // ContinuedEvent

	select {
	case <-feedDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for feed to complete after continue")
	}
}

func TestAdapter_StartPausedIfNoBreakpoints_NoPauseWhenBreakpointSet(t *testing.T) {
	adapter, runner, conn, reader := mustSetupDebugSession(t)
	adapter.SetStartPausedIfNoBreakpoints(true)

	// Set a breakpoint - the entry pause should NOT fire because a breakpoint exists.
	sendRequest(t, conn, &godap.SetBreakpointsRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 2, Type: "request"},
			Command:         "setBreakpoints",
		},
		Arguments: godap.SetBreakpointsArguments{
			Source:      godap.Source{Path: "/tmp/test/projection.js"},
			Breakpoints: []godap.SourceBreakpoint{{Line: 4}},
		},
	})
	readMessage(t, conn, reader) // SetBreakpointsResponse

	sendRequest(t, conn, &godap.ConfigurationDoneRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 3, Type: "request"},
			Command:         "configurationDone",
		},
	})
	readMessage(t, conn, reader) // ConfigurationDoneResponse

	feedDone := make(chan struct{})
	go func() {
		runner.ProcessOne(testFeedEvent)
		close(feedDone)
	}()

	// Should hit the user's breakpoint, not an entry pause.
	msg := readMessage(t, conn, reader)
	stopped, ok := msg.(*godap.StoppedEvent)
	if !ok {
		t.Fatalf("expected StoppedEvent at breakpoint, got %T", msg)
	}
	if stopped.Body.Reason != "breakpoint" {
		t.Fatalf("expected breakpoint reason (not entry pause), got %s", stopped.Body.Reason)
	}

	sendRequest(t, conn, &godap.ContinueRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 4, Type: "request"},
			Command:         "continue",
		},
		Arguments: godap.ContinueArguments{ThreadId: 1},
	})
	readMessage(t, conn, reader) // ContinueResponse
	readMessage(t, conn, reader) // ContinuedEvent

	select {
	case <-feedDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for feed to complete")
	}
	_ = adapter
}

func TestAdapter_StartPausedIfNoBreakpoints_PausesAfterClearingBreakpoints(t *testing.T) {
	adapter, runner, conn, reader := mustSetupDebugSession(t)
	adapter.SetStartPausedIfNoBreakpoints(true)

	// Set then clear breakpoints. Count should return to zero, entry pause fires.
	sendRequest(t, conn, &godap.SetBreakpointsRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 2, Type: "request"},
			Command:         "setBreakpoints",
		},
		Arguments: godap.SetBreakpointsArguments{
			Source:      godap.Source{Path: "/tmp/test/projection.js"},
			Breakpoints: []godap.SourceBreakpoint{{Line: 4}},
		},
	})
	readMessage(t, conn, reader)

	sendRequest(t, conn, &godap.SetBreakpointsRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 3, Type: "request"},
			Command:         "setBreakpoints",
		},
		Arguments: godap.SetBreakpointsArguments{
			Source:      godap.Source{Path: "/tmp/test/projection.js"},
			Breakpoints: []godap.SourceBreakpoint{},
		},
	})
	readMessage(t, conn, reader)

	sendRequest(t, conn, &godap.ConfigurationDoneRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 4, Type: "request"},
			Command:         "configurationDone",
		},
	})
	readMessage(t, conn, reader)

	feedDone := make(chan struct{})
	go func() {
		runner.ProcessOne(testFeedEvent)
		close(feedDone)
	}()

	msg := readMessage(t, conn, reader)
	stopped, ok := msg.(*godap.StoppedEvent)
	if !ok {
		t.Fatalf("expected StoppedEvent at entry, got %T", msg)
	}
	if stopped.Body.Reason != "entry" {
		t.Fatalf("expected entry reason after clearing breakpoints, got %s", stopped.Body.Reason)
	}

	sendRequest(t, conn, &godap.ContinueRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 5, Type: "request"},
			Command:         "continue",
		},
		Arguments: godap.ContinueArguments{ThreadId: 1},
	})
	readMessage(t, conn, reader)
	readMessage(t, conn, reader)

	select {
	case <-feedDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for feed to complete")
	}
}

func TestAdapter_StartPausedIfNoBreakpoints_DisabledByDefault(t *testing.T) {
	_, runner, conn, reader := mustSetupDebugSession(t)

	sendRequest(t, conn, &godap.ConfigurationDoneRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 2, Type: "request"},
			Command:         "configurationDone",
		},
	})
	readMessage(t, conn, reader) // ConfigurationDoneResponse

	feedDone := make(chan struct{})
	go func() {
		runner.ProcessOne(testFeedEvent)
		close(feedDone)
	}()

	// No flag, no breakpoints - should run straight through.
	select {
	case <-feedDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for feed to complete")
	}
}

func TestAdapter_EmitStateIfChanged(t *testing.T) {
	adapter, runner, conn, reader := mustSetupDebugSession(t)

	sendRequest(t, conn, &godap.ConfigurationDoneRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 2, Type: "request"},
			Command:         "configurationDone",
		},
	})
	readMessage(t, conn, reader) // ConfigurationDoneResponse

	// First call: empty state still differs from "never seen any state",
	// so the editor receives a baseline event.
	adapter.EmitStateIfChanged()
	body := readCustomEvent(t, conn, reader, "gaffer/state")
	if body["state"] != nil {
		t.Fatalf("expected empty initial state, got %+v", body)
	}

	// Re-emitting with the same (still empty) state is a no-op.
	adapter.EmitStateIfChanged()
	expectNoMessage(t, conn)

	// Process an event that mutates state.
	runner.ProcessOne(testFeedEvent)
	adapter.EmitStateIfChanged()
	body = readCustomEvent(t, conn, reader, "gaffer/state")
	if body["state"] == nil {
		t.Fatalf("expected state body, got %+v", body)
	}

	// Re-emitting with no state movement is a no-op.
	adapter.EmitStateIfChanged()
	expectNoMessage(t, conn)
}

func TestAdapter_EmitStatsIfChanged(t *testing.T) {
	adapter, runner, conn, reader := mustSetupDebugSession(t)

	sendRequest(t, conn, &godap.ConfigurationDoneRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 2, Type: "request"},
			Command:         "configurationDone",
		},
	})
	readMessage(t, conn, reader) // ConfigurationDoneResponse

	// Before any events: no emit (stats unchanged from zero).
	adapter.EmitStatsIfChanged()
	expectNoMessage(t, conn)

	// Process an event that the projection handles. handled goes 0->1
	// so a stats event fires.
	runner.ProcessOne(testFeedEvent)
	adapter.EmitStatsIfChanged()
	stats := readCustomEvent(t, conn, reader, "gaffer/stats")
	if stats["handled"] != float64(1) || stats["errors"] != float64(0) {
		t.Fatalf("expected handled=1 errors=0, got %+v", stats)
	}

	// Re-emitting with no new activity is a no-op.
	adapter.EmitStatsIfChanged()
	expectNoMessage(t, conn)

	// A pure-skip event moves EventStats.Skipped but NOT any field we
	// emit. No wire traffic - the editor's view didn't change.
	skippedEvent := `{"eventType":"NotHandled","streamId":"stream-1","sequenceNumber":1,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000001","created":"2026-01-01T00:00:00Z"}`
	runner.ProcessOne(skippedEvent)
	adapter.EmitStatsIfChanged()
	expectNoMessage(t, conn)
}

func TestAdapter_EmitStatsIfChanged_Quirks(t *testing.T) {
	// A projection returning a bare string as state fires quirk.serialize.rawString
	// at runtime (no log, so no output event to race the stats read), which the
	// adapter surfaces as a distinct count.
	adapter, runner, conn, reader := mustSetupDebugSessionSource(t,
		"fromAll().when({\nItemAdded(s, e) {\nreturn 'raw';\n}\n})")

	sendRequest(t, conn, &godap.ConfigurationDoneRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 2, Type: "request"},
			Command:         "configurationDone",
		},
	})
	readMessage(t, conn, reader) // ConfigurationDoneResponse

	runner.ProcessOne(testFeedEvent)
	adapter.EmitStatsIfChanged()
	stats := readCustomEvent(t, conn, reader, "gaffer/stats")
	if stats["quirks"] != float64(1) {
		t.Fatalf("quirks: got %v, want 1", stats["quirks"])
	}

	// A second event fires the same code; the distinct count stays 1, so
	// the quirk field alone wouldn't re-trigger an emit (handled moves it).
	runner.ProcessOne(testFeedEvent)
	adapter.EmitStatsIfChanged()
	stats = readCustomEvent(t, conn, reader, "gaffer/stats")
	if stats["quirks"] != float64(1) {
		t.Fatalf("quirks after 2nd event: got %v, want 1 (distinct)", stats["quirks"])
	}
}

func TestAdapter_EmitStatsIfChanged_FixtureMode(t *testing.T) {
	adapter, runner, conn, reader := mustSetupDebugSession(t)
	adapter.SetFixtureMode(true)

	sendRequest(t, conn, &godap.ConfigurationDoneRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 2, Type: "request"},
			Command:         "configurationDone",
		},
	})
	readMessage(t, conn, reader) // ConfigurationDoneResponse

	// Pure-skip events drive Skipped but not Handled. In fixture mode
	// that's diagnostic, so the wire emit fires.
	skippedEvent := `{"eventType":"NotHandled","streamId":"stream-1","sequenceNumber":1,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000001","created":"2026-01-01T00:00:00Z"}`
	runner.ProcessOne(skippedEvent)
	adapter.EmitStatsIfChanged()

	stats := readCustomEvent(t, conn, reader, "gaffer/stats")
	if stats["handled"] != float64(0) {
		t.Fatalf("handled: got %v, want 0", stats["handled"])
	}
	if stats["skipped"] != float64(1) {
		t.Fatalf("skipped: got %v, want 1", stats["skipped"])
	}
	byReason, ok := stats["skippedByReason"].(map[string]any)
	if !ok || len(byReason) == 0 {
		t.Fatalf("expected skippedByReason map, got %+v", stats["skippedByReason"])
	}
}

// Reproduces F6 (manual testing): after entry pause, Continue should let
// the source keep feeding subsequent events without further pauses.
func TestAdapter_StartPausedIfNoBreakpoints_ContinueProcessesSubsequentEvents(t *testing.T) {
	adapter, runner, conn, reader := mustSetupDebugSession(t)
	adapter.SetStartPausedIfNoBreakpoints(true)

	sendRequest(t, conn, &godap.ConfigurationDoneRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 2, Type: "request"},
			Command:         "configurationDone",
		},
	})
	readMessage(t, conn, reader) // ConfigurationDoneResponse

	feedDone := make(chan struct{})
	go func() {
		runner.ProcessOne(testFeedEvent)
		runner.ProcessOne(testFeedEvent)
		runner.ProcessOne(testFeedEvent)
		close(feedDone)
	}()

	// First event triggers entry pause.
	msg := readMessage(t, conn, reader)
	stopped, ok := msg.(*godap.StoppedEvent)
	if !ok {
		t.Fatalf("expected StoppedEvent at entry, got %T", msg)
	}
	if stopped.Body.Reason != "entry" {
		t.Fatalf("expected entry reason, got %s", stopped.Body.Reason)
	}

	sendRequest(t, conn, &godap.ContinueRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 3, Type: "request"},
			Command:         "continue",
		},
		Arguments: godap.ContinueArguments{ThreadId: 1},
	})
	readMessage(t, conn, reader) // ContinueResponse
	readMessage(t, conn, reader) // ContinuedEvent

	// All three events must complete without any further pauses.
	select {
	case <-feedDone:
	case <-time.After(5 * time.Second):
		t.Fatal("F6: events 2 and 3 did not process after Continue from entry pause")
	}
}

// Reproduces F4 (manual testing): on disconnect the dev command's
// ctx-cancellation goroutine used to call r.Destroy(), tearing down
// the session before the post-run summary path could read state.
// CollectState then panicked with "use of destroyed session".
//
// r.Unblock is the cancellation-path replacement: it clears
// breakpoints + resumes if paused, but leaves the session alive so
// state can still be collected.
func TestAdapter_UnblockReleasesPausedFeedAndKeepsSessionAlive(t *testing.T) {
	adapter, runner, conn, reader := mustSetupDebugSession(t)
	adapter.SetStartPausedIfNoBreakpoints(true)

	sendRequest(t, conn, &godap.ConfigurationDoneRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 2, Type: "request"},
			Command:         "configurationDone",
		},
	})
	readMessage(t, conn, reader) // ConfigurationDoneResponse

	feedDone := make(chan struct{})
	go func() {
		runner.ProcessOne(testFeedEvent)
		close(feedDone)
	}()

	// First event triggers entry pause - confirms feed is blocked.
	msg := readMessage(t, conn, reader)
	if _, ok := msg.(*godap.StoppedEvent); !ok {
		t.Fatalf("expected StoppedEvent at entry, got %T", msg)
	}

	// Simulate the dev command's ctx-cancellation handler. Must
	// release the paused feed without destroying the session.
	if err := runner.Unblock(); err != nil {
		t.Fatalf("Unblock: %v", err)
	}

	select {
	case <-feedDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Unblock did not release the paused feed")
	}

	// The session must still be queryable - this is what dev.go's
	// post-run summary write needs after disconnect. CollectState
	// panicked here under the previous Destroy-based teardown.
	summary := runner.CollectState()
	_ = summary // smoke test: must not panic
}
