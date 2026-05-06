package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
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
	err := conn.Call(ctx, "textDocument/foreachStream", nil, nil)
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

// startServerWithStore runs a server in a goroutine over the given
// stream and returns its document store handle plus the result
// channel from Run. Convenient for tests that want to assert on
// store state after driving lifecycle messages.
func startServerWithStore(ctx context.Context, stream io.ReadWriteCloser, opts ServerOptions) (*Server, <-chan error) {
	srv := NewServer(opts)
	done := make(chan error, 1)
	go func() {
		done <- srv.Run(ctx, stream)
	}()
	return srv, done
}

func TestServer_DidOpenStoresTheBuffer(t *testing.T) {
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	conn := newClientConn(ctx, cli)
	defer func() { _ = conn.Close() }()

	if err := conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	uri := "file:///workspace/gaffer.toml"
	if err := conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, LanguageID: "toml", Version: 1, Text: "engine_version = 2"},
	}); err != nil {
		t.Fatalf("didOpen: %v", err)
	}

	// didOpen is a notification - wait briefly for the handler to
	// run before asserting on store state.
	waitFor(t, func() bool {
		state, ok := server.docs.Get(uri)
		return ok && state.Source == sourceMemory && state.Content == "engine_version = 2"
	}, time.Second)

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_DidChangeUpdatesContent(t *testing.T) {
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	conn := newClientConn(ctx, cli)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{})

	uri := "file:///a.toml"
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, LanguageID: "toml", Version: 1, Text: "first"},
	})
	_ = conn.Notify(ctx, MethodDidChange, &DidChangeTextDocumentParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: uri, Version: 2},
		ContentChanges: []TextDocumentContentChangeEvent{{Text: "second"}},
	})

	waitFor(t, func() bool {
		state, ok := server.docs.Get(uri)
		return ok && state.Content == "second"
	}, time.Second)

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_DidChangeMultipleEventsTakesLast(t *testing.T) {
	// Pin the choice: under full sync, if a client sends multiple
	// content-change events in one didChange (spec says SHOULD
	// send only one but doesn't forbid more), we treat the last
	// one as authoritative. A future "first wins" refactor breaks
	// this test loudly.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	conn := newClientConn(ctx, cli)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{})

	uri := "file:///a.toml"
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, Text: "initial"},
	})
	_ = conn.Notify(ctx, MethodDidChange, &DidChangeTextDocumentParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: 2},
		ContentChanges: []TextDocumentContentChangeEvent{
			{Text: "intermediate"},
			{Text: "final"},
		},
	})

	waitFor(t, func() bool {
		state, ok := server.docs.Get(uri)
		return ok && state.Content == "final"
	}, time.Second)

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_DidCloseRemovesFromStore(t *testing.T) {
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	conn := newClientConn(ctx, cli)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{})

	uri := "file:///a.toml"
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, LanguageID: "toml", Version: 1, Text: "x"},
	})
	// Wait for didOpen to land before sending didClose so we know
	// the close is acting on a present URI.
	waitFor(t, func() bool {
		_, ok := server.docs.Get(uri)
		return ok
	}, time.Second)
	_ = conn.Notify(ctx, MethodDidClose, &DidCloseTextDocumentParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	})
	waitFor(t, func() bool {
		_, ok := server.docs.Get(uri)
		return !ok
	}, time.Second)

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_DidSaveIsAccepted(t *testing.T) {
	// V1 advertises bare-int TextDocumentSync (no save field), so
	// well-behaved clients won't send didSave at all. But some
	// clients send it regardless. The server falls through to the
	// default-branch MethodNotFound response, which jsonrpc2
	// silently drops for notifications. Verify the connection
	// stays alive and the store is untouched.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	conn := newClientConn(ctx, cli)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{})

	uri := "file:///a.toml"
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, Text: "x"},
	})
	waitFor(t, func() bool {
		_, ok := server.docs.Get(uri)
		return ok
	}, time.Second)
	if err := conn.Notify(ctx, MethodDidSave, &DidSaveTextDocumentParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}); err != nil {
		t.Fatalf("didSave: %v", err)
	}
	state, _ := server.docs.Get(uri)
	if state.Content != "x" {
		t.Errorf("didSave should not alter content: got %q", state.Content)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

// waitFor polls cond until it returns true or `timeout` elapses,
// failing the test if the latter. Used to bridge the async gap
// between sending an LSP notification and its handler completing.
func waitFor(t *testing.T, cond func() bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("waitFor: condition never became true")
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

func TestExtractRoots_FiltersNonFileURIs(t *testing.T) {
	// vscode-vfs and untitled URIs aren't on the local fs - the
	// walker would emit log noise trying to ReadDir them. Drop
	// silently so a multi-folder workspace mixing local and
	// remote roots still walks the local ones.
	got := extractRoots(InitializeParams{
		WorkspaceFolders: []WorkspaceFolder{
			{URI: "file:///local", Name: "local"},
			{URI: "vscode-vfs://github/owner/repo", Name: "remote"},
			{URI: "untitled:Untitled-1", Name: "buffer"},
			{URI: "file:///also-local", Name: "also"},
		},
	})
	want := []string{"/local", "/also-local"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("extractRoots: got %v want %v", got, want)
	}
}

func TestExtractRoots_PrefersWorkspaceFoldersOverRootURI(t *testing.T) {
	// When both are present, WorkspaceFolders wins per the LSP
	// spec. RootURI is the older single-folder fallback.
	got := extractRoots(InitializeParams{
		RootURI: "file:///old",
		WorkspaceFolders: []WorkspaceFolder{
			{URI: "file:///new", Name: "new"},
		},
	})
	if !reflect.DeepEqual(got, []string{"/new"}) {
		t.Errorf("expected WorkspaceFolders to win, got %v", got)
	}
}

func TestServer_FallsBackToRootURIWhenWorkspaceFoldersAbsent(t *testing.T) {
	// Older LSP clients may not send WorkspaceFolders. The server
	// must still walk the rootUri so the lens contract holds for
	// those clients.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	root := t.TempDir()
	server := NewServer(ServerOptions{})
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx, srv) }()
	conn := newClientConn(ctx, cli)
	defer func() { _ = conn.Close() }()

	if err := conn.Call(ctx, MethodInitialize, &InitializeParams{
		RootURI: pathToURI(root),
	}, &InitializeResult{}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if !reflect.DeepEqual(server.roots, []string{root}) {
		t.Errorf("roots: got %v want [%q]", server.roots, root)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_SpawnRefusesAfterDraining(t *testing.T) {
	// Race regression: a handler that calls spawn after Run's
	// defer set draining=true must not increment wg (Add(1) racing
	// Wait is undefined under sync.WaitGroup's contract). Verify
	// spawn returns false in that state.
	server := NewServer(ServerOptions{})
	server.draining = true
	if server.spawn(func() { t.Error("spawn should not have run fn") }) {
		t.Error("spawn returned true while draining")
	}
}

func TestServer_DidOpenSameURITwiceOverwritesBuffer(t *testing.T) {
	// LSP spec says didOpen of an already-open URI without an
	// intervening didClose is a client bug. Pin our actual
	// behavior - the second Open replaces the first - so a
	// future contract change is loud.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	conn := newClientConn(ctx, cli)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{})
	uri := "file:///x.toml"
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, Text: "first"},
	})
	waitFor(t, func() bool {
		state, ok := server.docs.Get(uri)
		return ok && state.Content == "first"
	}, time.Second)
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, Text: "second"},
	})
	waitFor(t, func() bool {
		state, ok := server.docs.Get(uri)
		return ok && state.Content == "second"
	}, time.Second)

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
}

func TestServer_CtxCancelMidSessionIsClean(t *testing.T) {
	// SIGINT (or any caller-side ctx cancel) mid-session must
	// not surface as a protocol error - the client never had a
	// chance to send shutdown, so blaming them is wrong. Pin the
	// fix for the bug where ctx-cancel was reported as
	// "client disconnected without sending shutdown".
	srv, cli := pipePair()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := startServer(ctx, srv, ServerOptions{})
	conn := newClientConn(ctx, cli)
	defer func() { _ = conn.Close() }()
	if err := conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{}); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	cancel() // <-- caller pulls the rug

	if err := <-done; err != nil {
		t.Fatalf("expected clean exit on ctx cancel, got %v", err)
	}
}
