package dap

import (
	"bufio"
	"encoding/json"
	"net"
	"testing"
	"time"

	godap "github.com/google/go-dap"
)

func mustStartServer(t *testing.T, handler Handler) (*Server, net.Conn) {
	t.Helper()
	srv, err := NewServer("127.0.0.1:0", handler)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	connCh := make(chan net.Conn, 1)
	go func() {
		conn, err := net.Dial("tcp", srv.Addr().String())
		if err != nil {
			t.Error(err)
			return
		}
		connCh <- conn
	}()

	go func() { _ = srv.Serve() }()

	select {
	case conn := <-connCh:
		t.Cleanup(func() { _ = conn.Close() })
		return srv, conn
	case <-time.After(5 * time.Second):
		t.Fatal("timed out connecting to server")
		return nil, nil
	}
}

func sendRequest(t *testing.T, conn net.Conn, msg godap.Message) {
	t.Helper()
	if err := godap.WriteProtocolMessage(conn, msg); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}
}

var testCodec = func() *godap.Codec {
	c := godap.NewCodec()
	RegisterCustomRequests(c)
	return c
}()

func readMessage(t *testing.T, conn net.Conn, reader *bufio.Reader) godap.Message {
	t.Helper()
	for {
		data, err := godap.ReadBaseMessage(reader)
		if err != nil {
			t.Fatalf("failed to read message: %v", err)
		}
		msg, err := testCodec.DecodeMessage(data)
		if err != nil {
			continue
		}
		return msg
	}
}

// readCustomEvent reads the next message off the wire and decodes it as
// a gaffer custom event with the given event name. The codec doesn't
// know about gaffer/* event subtypes, so this bypasses it and parses
// the JSON envelope directly.
func readCustomEvent(t *testing.T, conn net.Conn, reader *bufio.Reader, expected string) map[string]any {
	t.Helper()
	data, err := godap.ReadBaseMessage(reader)
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}
	var envelope struct {
		Type  string         `json:"type"`
		Event string         `json:"event"`
		Body  map[string]any `json:"body"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("failed to decode custom event: %v", err)
	}
	if envelope.Type != "event" || envelope.Event != expected {
		t.Fatalf("expected event %s, got %s/%s: %s", expected, envelope.Type, envelope.Event, string(data))
	}
	return envelope.Body
}

// expectNoMessage fails if any message arrives within a short window.
// Used to assert that an action did not produce a wire send.
func expectNoMessage(t *testing.T, conn net.Conn) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()
	buf := make([]byte, 1)
	if n, err := conn.Read(buf); err == nil && n > 0 {
		t.Fatalf("expected no message, got byte %q", buf[0])
	}
}

func TestInitializeHandshake(t *testing.T) {
	_, conn := mustStartServer(t, Handler{})
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

	// Should get InitializeResponse
	msg1 := readMessage(t, conn, reader)
	resp, ok := msg1.(*godap.InitializeResponse)
	if !ok {
		t.Fatalf("expected InitializeResponse, got %T", msg1)
	}
	if !resp.Success {
		t.Fatal("initialize response not successful")
	}
	if !resp.Body.SupportsConfigurationDoneRequest {
		t.Fatal("expected supportsConfigurationDoneRequest")
	}

	// Should get InitializedEvent
	msg2 := readMessage(t, conn, reader)
	_, ok = msg2.(*godap.InitializedEvent)
	if !ok {
		t.Fatalf("expected InitializedEvent, got %T", msg2)
	}
}

func TestSetExceptionBreakpointsStub(t *testing.T) {
	_, conn := mustStartServer(t, Handler{})
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

	sendRequest(t, conn, &godap.SetExceptionBreakpointsRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 2, Type: "request"},
			Command:         "setExceptionBreakpoints",
		},
		Arguments: godap.SetExceptionBreakpointsArguments{
			Filters: []string{},
		},
	})

	msg := readMessage(t, conn, reader)
	resp, ok := msg.(*godap.SetExceptionBreakpointsResponse)
	if !ok {
		t.Fatalf("expected SetExceptionBreakpointsResponse, got %T", msg)
	}
	if !resp.Success {
		t.Fatal("expected success")
	}
}

func TestThreadsReturnsSingleThread(t *testing.T) {
	_, conn := mustStartServer(t, Handler{})
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
	readMessage(t, conn, reader)
	readMessage(t, conn, reader)

	sendRequest(t, conn, &godap.ThreadsRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 2, Type: "request"},
			Command:         "threads",
		},
	})

	msg := readMessage(t, conn, reader)
	resp, ok := msg.(*godap.ThreadsResponse)
	if !ok {
		t.Fatalf("expected ThreadsResponse, got %T", msg)
	}
	if len(resp.Body.Threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(resp.Body.Threads))
	}
	if resp.Body.Threads[0].Name != "projection" {
		t.Fatalf("expected thread name 'projection', got %s", resp.Body.Threads[0].Name)
	}
}

func TestDisconnect(t *testing.T) {
	disconnected := make(chan struct{})
	_, conn := mustStartServer(t, Handler{
		OnDisconnect: func(s *Server, req *godap.DisconnectRequest) {
			resp := &godap.DisconnectResponse{}
			resp.Response = NewResponse(req.Seq, req.Command)
			s.Send(resp)
			close(disconnected)
		},
	})
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
	readMessage(t, conn, reader)
	readMessage(t, conn, reader)

	sendRequest(t, conn, &godap.DisconnectRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 2, Type: "request"},
			Command:         "disconnect",
		},
	})

	msg := readMessage(t, conn, reader)
	_, ok := msg.(*godap.DisconnectResponse)
	if !ok {
		t.Fatalf("expected DisconnectResponse, got %T", msg)
	}

	select {
	case <-disconnected:
	case <-time.After(5 * time.Second):
		t.Fatal("disconnect handler not called")
	}
}

func TestSendDropsWhenBufferFull(t *testing.T) {
	// A wedged buffer means the editor stopped reading; Send must
	// drop rather than block (which would stall the engine behind a
	// doomed connection).
	srv := &Server{
		sendCh:   make(chan godap.Message, 1),
		sendOpen: true,
	}
	srv.Send(&godap.OutputEvent{Event: NewEvent("output")})
	if len(srv.sendCh) != 1 {
		t.Fatalf("expected first send to land in buffer, got len=%d", len(srv.sendCh))
	}
	// Second send: buffer full, must drop without blocking or panicking.
	done := make(chan struct{})
	go func() {
		srv.Send(&godap.OutputEvent{Event: NewEvent("output")})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Send blocked on full buffer")
	}
	if len(srv.sendCh) != 1 {
		t.Fatalf("expected dropped send to leave len=1, got %d", len(srv.sendCh))
	}
}

func TestSendDropsWhenClosed(t *testing.T) {
	// sendOpen=false (post-Serve teardown) must short-circuit Send so
	// late callbacks from the engine don't panic on a closed channel.
	srv := &Server{
		sendCh:   make(chan godap.Message, 1),
		sendOpen: false,
	}
	close(srv.sendCh)
	// Would panic without the sendOpen guard.
	srv.Send(&godap.OutputEvent{Event: NewEvent("output")})
}

func TestUnknownRequestReturnsError(t *testing.T) {
	_, conn := mustStartServer(t, Handler{})
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

	// LaunchRequest isn't in the dispatch switch (we're attach-only),
	// so it falls through to the default "not supported" branch.
	sendRequest(t, conn, &godap.LaunchRequest{
		Request: godap.Request{
			ProtocolMessage: godap.ProtocolMessage{Seq: 2, Type: "request"},
			Command:         "launch",
		},
	})

	msg := readMessage(t, conn, reader)
	resp, ok := msg.(*godap.ErrorResponse)
	if !ok {
		t.Fatalf("expected ErrorResponse, got %T", msg)
	}
	if resp.Success {
		t.Fatal("expected failure response")
	}
	if resp.Message != "not supported" {
		t.Fatalf("expected message 'not supported', got %q", resp.Message)
	}
}

func TestSequenceNumbersAutoAssigned(t *testing.T) {
	_, conn := mustStartServer(t, Handler{})
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

	msg1 := readMessage(t, conn, reader) // InitializeResponse
	msg2 := readMessage(t, conn, reader) // InitializedEvent

	seq1 := msg1.GetSeq()
	seq2 := msg2.GetSeq()

	if seq1 == 0 {
		t.Fatal("response seq should not be 0")
	}
	if seq2 == 0 {
		t.Fatal("event seq should not be 0")
	}
	if seq2 <= seq1 {
		t.Fatalf("event seq (%d) should be greater than response seq (%d)", seq2, seq1)
	}
}
