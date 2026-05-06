package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
)

// pipePair returns two ReadWriteClosers connected back-to-back via
// io.Pipe. Used to drive the server in-process without spawning a
// subprocess - lets us pin behavior at the protocol level without
// stdio plumbing.
func pipePair() (a, b io.ReadWriteCloser) {
	ar, bw := io.Pipe()
	br, aw := io.Pipe()
	return &rwc{r: ar, w: aw}, &rwc{r: br, w: bw}
}

type rwc struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func (p *rwc) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *rwc) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p *rwc) Close() error {
	_ = p.r.Close()
	return p.w.Close()
}

// startServer runs a server in a goroutine over the given stream
// and returns a channel that delivers the Run result.
func startServer(ctx context.Context, stream io.ReadWriteCloser, opts ServerOptions) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- NewServer(opts).Run(ctx, stream)
	}()
	return done
}

// newClientConn wires a jsonrpc2 client over the given stream.
func newClientConn(ctx context.Context, stream io.ReadWriteCloser) *jsonrpc2.Conn {
	return jsonrpc2.NewConn(
		ctx,
		jsonrpc2.NewBufferedStream(stream, jsonrpc2.VSCodeObjectCodec{}),
		// Server-initiated messages aren't expected in V1.
		jsonrpc2.HandlerWithError(func(_ context.Context, _ *jsonrpc2.Conn, _ *jsonrpc2.Request) (interface{}, error) {
			return nil, nil
		}),
	)
}

func TestServer_InitializeReturnsCapabilities(t *testing.T) {
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := startServer(ctx, srv, ServerOptions{Version: "test"})
	conn := newClientConn(ctx, cli)
	defer func() { _ = conn.Close() }()

	var result InitializeResult
	if err := conn.Call(ctx, MethodInitialize, &InitializeParams{}, &result); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if result.Capabilities.TextDocumentSync != 1 {
		t.Errorf("textDocumentSync: got %d, want 1 (full sync)", result.Capabilities.TextDocumentSync)
	}
	if result.ServerInfo.Name != "gaffer-lsp" {
		t.Errorf("serverInfo.name: got %q want gaffer-lsp", result.ServerInfo.Name)
	}
	if result.ServerInfo.Version != "test" {
		t.Errorf("serverInfo.version: got %q want test", result.ServerInfo.Version)
	}

	// Drive a clean shutdown. Note we DON'T close the client side
	// after Notify(exit) - the server is expected to drive the
	// disconnect itself once it sees `exit`.
	if err := conn.Call(ctx, MethodShutdown, nil, nil); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := conn.Notify(ctx, MethodExit, nil); err != nil {
		t.Fatalf("exit: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("server.Run returned error: %v", err)
	}
}

func TestServer_ExitDrivesDisconnect(t *testing.T) {
	// Reproducer for the bug where the server hung waiting for the
	// client to also close stdin after sending `exit`. Per LSP
	// spec, the server is expected to terminate on exit. We assert
	// Run returns within a short timeout WITHOUT the client having
	// to call conn.Close().
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := startServer(ctx, srv, ServerOptions{})
	conn := newClientConn(ctx, cli)
	defer func() { _ = conn.Close() }()

	if err := conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := conn.Call(ctx, MethodShutdown, nil, nil); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := conn.Notify(ctx, MethodExit, nil); err != nil {
		t.Fatalf("exit: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected clean shutdown, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server.Run hung after exit notification")
	}
}

func TestServer_ContextCancellationStopsRun(t *testing.T) {
	// SIGINT path: cmd/lsp.go cancels ctx; Run must return rather
	// than blocking on stdin.
	srv, cli := pipePair()
	defer func() { _ = cli.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	done := startServer(ctx, srv, ServerOptions{})

	conn := newClientConn(ctx, cli)
	defer func() { _ = conn.Close() }()
	if err := conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{}); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("server.Run hung after ctx cancellation")
	}
}

func TestServer_DoubleInitializeIsRejected(t *testing.T) {
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := startServer(ctx, srv, ServerOptions{})
	conn := newClientConn(ctx, cli)
	defer func() { _ = conn.Close() }()

	var first InitializeResult
	if err := conn.Call(ctx, MethodInitialize, &InitializeParams{}, &first); err != nil {
		t.Fatalf("first initialize: %v", err)
	}
	var second InitializeResult
	err := conn.Call(ctx, MethodInitialize, &InitializeParams{}, &second)
	if err == nil {
		t.Fatal("expected second initialize to fail")
	}
	var jrpcErr *jsonrpc2.Error
	if !errors.As(err, &jrpcErr) || jrpcErr.Code != jsonrpc2.CodeInvalidRequest {
		t.Errorf("expected jsonrpc2 InvalidRequest, got %v", err)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_UnknownMethodReturnsMethodNotFound(t *testing.T) {
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := startServer(ctx, srv, ServerOptions{})
	conn := newClientConn(ctx, cli)
	defer func() { _ = conn.Close() }()

	if err := conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	err := conn.Call(ctx, "textDocument/didOpen", nil, nil)
	if err == nil {
		t.Fatal("expected unknown-method error")
	}
	var jrpcErr *jsonrpc2.Error
	if !errors.As(err, &jrpcErr) || jrpcErr.Code != jsonrpc2.CodeMethodNotFound {
		t.Errorf("expected MethodNotFound, got %v", err)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_ExitWithoutShutdownAfterInitializeIsAProtocolError(t *testing.T) {
	// LSP spec: if the client sends exit without shutdown after
	// initialize, the session is unclean. Run returns an error so
	// callers can map to a non-zero exit code.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := startServer(ctx, srv, ServerOptions{})
	conn := newClientConn(ctx, cli)
	defer func() { _ = conn.Close() }()

	if err := conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	// Skip shutdown; send exit directly. Server drives disconnect
	// itself and exitStatus reports the protocol violation.
	if err := conn.Notify(ctx, MethodExit, nil); err != nil {
		t.Fatalf("exit: %v", err)
	}
	if err := <-done; err == nil {
		t.Fatal("expected protocol error on exit without prior shutdown")
	}
}

func TestServer_DisconnectWithoutShutdownAfterInitializeIsAProtocolError(t *testing.T) {
	// Variant: client crashes / hangs up after initialize without
	// either shutdown or exit. Same protocol-error outcome - no
	// graceful close was negotiated.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := startServer(ctx, srv, ServerOptions{})
	conn := newClientConn(ctx, cli)
	if err := conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	_ = conn.Close()
	if err := <-done; err == nil {
		t.Fatal("expected protocol error on unclean disconnect")
	}
}

func TestServer_ExitBeforeInitializeIsClean(t *testing.T) {
	// LSP spec: exit before initialize is exit code 0 - no session
	// was ever established, nothing to leak.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := startServer(ctx, srv, ServerOptions{})
	conn := newClientConn(ctx, cli)
	defer func() { _ = conn.Close() }()

	if err := conn.Notify(ctx, MethodExit, nil); err != nil {
		t.Fatalf("exit: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("expected clean exit, got %v", err)
	}
}

func TestServer_AcceptsNullWorkspaceFolders(t *testing.T) {
	// Single-folder mode legitimately sends `"workspaceFolders": null`.
	// JSON unmarshal accepts null for slices in Go (-> nil); pin so a
	// future tightening doesn't reject legitimate clients.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := startServer(ctx, srv, ServerOptions{})
	conn := newClientConn(ctx, cli)
	defer func() { _ = conn.Close() }()

	raw := json.RawMessage(`{"capabilities": {}, "workspaceFolders": null}`)
	if err := conn.Call(ctx, MethodInitialize, raw, &InitializeResult{}); err != nil {
		t.Fatalf("initialize with null workspaceFolders: %v", err)
	}
	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_DisconnectBeforeInitializeIsClean(t *testing.T) {
	// Client connects and disconnects without initializing - no
	// state to lose, no protocol violation.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := startServer(ctx, srv, ServerOptions{})
	_ = cli.Close()
	if err := <-done; err != nil {
		t.Fatalf("expected clean exit, got %v", err)
	}
	_ = srv
}
