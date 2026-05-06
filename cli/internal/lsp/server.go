package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/sourcegraph/jsonrpc2"
)

// ServerOptions configures a Server before Run starts the message
// loop. Zero-value is usable; callers override individual fields.
type ServerOptions struct {
	// Version surfaced via InitializeResult.ServerInfo.Version.
	// Callers (e.g. cmd/lsp.go) inject the build version.
	Version string
}

// Server is the gaffer LSP server. One instance per stdio session;
// the message loop runs in Run and exits when the client closes
// stdin, sends shutdown+exit, or the run context is cancelled.
//
// Concurrency: jsonrpc2 dispatches each request in its own
// goroutine, so handler methods are called concurrently. Document-
// state coordination lives in chunks 2.2+; for now the server only
// handles initialize / initialized / shutdown / exit, which the
// LSP spec serializes by ordering.
type Server struct {
	opts ServerOptions

	mu          sync.Mutex
	initialized bool
	shutdownReq bool
	// exitCh closes when the client sends `exit`. Run selects on
	// this so the server tears down its connection without waiting
	// for the client to also close stdin (a well-behaved client
	// expects the server to drive disconnect on exit).
	exitCh chan struct{}
}

// NewServer constructs a server with the given options. Doesn't
// touch I/O; call Run to start the message loop.
func NewServer(opts ServerOptions) *Server {
	return &Server{
		opts:   opts,
		exitCh: make(chan struct{}),
	}
}

// Run drives the JSON-RPC message loop over the given stream until
// the client closes the connection, sends `exit`, or ctx is
// cancelled.
//
// Returns nil for a clean shutdown (shutdown then exit, or peer
// disconnect before initialize) and a non-nil error for protocol
// violations (initialize without prior shutdown then disconnect or
// exit). Callers map clean shutdown to exit code 0 and protocol
// errors to non-zero.
func (s *Server) Run(ctx context.Context, stream io.ReadWriteCloser) error {
	conn := jsonrpc2.NewConn(
		ctx,
		jsonrpc2.NewBufferedStream(stream, jsonrpc2.VSCodeObjectCodec{}),
		jsonrpc2.HandlerWithError(s.handle),
	)
	// Three ways out: peer disconnects, client sent exit (we drive
	// disconnect), or our run context was cancelled (e.g. SIGINT).
	// All three converge on closing the connection and waiting for
	// jsonrpc2 to drain in-flight handlers.
	select {
	case <-conn.DisconnectNotify():
	case <-s.exitCh:
		_ = conn.Close()
		<-conn.DisconnectNotify()
	case <-ctx.Done():
		_ = conn.Close()
		<-conn.DisconnectNotify()
	}
	return s.exitStatus()
}

// handle dispatches a single JSON-RPC message to the right method.
// jsonrpc2.HandlerWithError takes care of error/result wrapping.
func (s *Server) handle(ctx context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (interface{}, error) {
	switch req.Method {
	case MethodInitialize:
		return s.handleInitialize(ctx, req)
	case MethodInitialized:
		// Notification, no response. Currently a no-op; chunks 2.4+
		// will use it as the signal to register watchers.
		return nil, nil
	case MethodShutdown:
		return s.handleShutdown()
	case MethodExit:
		// Notification. Signal Run to tear down the connection -
		// LSP spec expects the server to terminate on exit, not
		// hang waiting for the client to also close stdin.
		s.signalExit()
		return nil, nil
	default:
		// CodeMethodNotFound is dropped by jsonrpc2 when the
		// inbound was a notification (no ID, no response slot).
		// For requests it surfaces as a proper JSON-RPC error.
		return nil, &jsonrpc2.Error{
			Code:    jsonrpc2.CodeMethodNotFound,
			Message: fmt.Sprintf("method not implemented: %s", req.Method),
		}
	}
}

func (s *Server) handleInitialize(_ context.Context, req *jsonrpc2.Request) (interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initialized {
		// LSP spec: a second initialize is a hard error.
		return nil, &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidRequest,
			Message: "initialize called twice",
		}
	}
	// Validate params shape now even though we don't use the
	// content yet - rejects malformed initialize requests upfront
	// rather than letting them through to be re-parsed (and
	// possibly fail differently) once chunks 2.2+ wire workspace
	// folders into the document store.
	if req.Params != nil {
		var params InitializeParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return nil, &jsonrpc2.Error{
				Code:    jsonrpc2.CodeInvalidParams,
				Message: err.Error(),
			}
		}
	}
	s.initialized = true
	return InitializeResult{
		Capabilities: ServerCapabilities{
			TextDocumentSync: 1, // full document sync (Decision 1)
		},
		ServerInfo: ServerInfo{
			Name:    "gaffer-lsp",
			Version: s.opts.Version,
		},
	}, nil
}

func (s *Server) handleShutdown() (interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shutdownReq = true
	return nil, nil
}

// signalExit closes exitCh idempotently. Multiple calls are safe -
// only the first close fires; subsequent calls are no-ops.
func (s *Server) signalExit() {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.exitCh:
		// already closed
	default:
		close(s.exitCh)
	}
}

// exitStatus is consulted at disconnect time to decide whether the
// session ended cleanly. LSP spec: exit without prior shutdown is a
// protocol violation (typically a crashed client).
func (s *Server) exitStatus() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initialized && !s.shutdownReq {
		return errors.New("client disconnected without sending shutdown")
	}
	return nil
}
