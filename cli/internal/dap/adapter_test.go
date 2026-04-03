package dap

import (
	"bufio"
	"net"
	"testing"
	"time"

	godap "github.com/google/go-dap"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
)

const testDebugOpts = `{"debug":true}`

const testFeedEvent = `{"eventType":"ItemAdded","streamId":"stream-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`

func mustSetupDebugSession(t *testing.T) (*DebugAdapter, *engine.Runner, net.Conn, *bufio.Reader) {
	t.Helper()
	opts := testDebugOpts
	source := "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function handler(s, e) {\ns.count++;\nreturn s;\n}\n})"
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
	if stResp.Body.StackFrames[0].Name != "handler" {
		t.Fatalf("expected handler, got %s", stResp.Body.StackFrames[0].Name)
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

func TestAdapter_SendTerminated(t *testing.T) {
	adapter, _, conn, reader := mustSetupDebugSession(t)

	sendRequest(t, conn, &godap.ConfigurationDoneRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 2, Type: "request"},
			Command:         "configurationDone",
		},
	})
	readMessage(t, conn, reader) // ConfigurationDoneResponse

	adapter.SendTerminated()

	msg := readMessage(t, conn, reader)
	_, ok := msg.(*godap.TerminatedEvent)
	if !ok {
		t.Fatalf("expected TerminatedEvent, got %T", msg)
	}

	msg = readMessage(t, conn, reader)
	_, ok = msg.(*godap.ExitedEvent)
	if !ok {
		t.Fatalf("expected ExitedEvent, got %T", msg)
	}
}
