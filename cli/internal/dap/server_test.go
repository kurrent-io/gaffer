package dap

import (
	"bufio"
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

func readMessage(t *testing.T, conn net.Conn, reader *bufio.Reader) godap.Message {
	t.Helper()
	msg, err := godap.ReadProtocolMessage(reader)
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}
	return msg
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
