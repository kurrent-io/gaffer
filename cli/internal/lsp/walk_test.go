package lsp

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
)

// capturedRequest records a single server-pushed request to the
// client. Distinct from capturedNotify - requests carry IDs and
// require responses.
type capturedRequest struct {
	Method string
	Params json.RawMessage
}

// clientStub is a richer test-side handler than notifyCapture: it
// records both notifications AND requests, and answers requests
// with a nil result so server-side conn.Call doesn't hang.
type clientStub struct {
	mu       sync.Mutex
	notifs   []capturedNotify
	requests []capturedRequest
}

func (c *clientStub) handler(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (interface{}, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var raw json.RawMessage
	if req.Params != nil {
		raw = *req.Params
	}
	if req.Notif {
		c.notifs = append(c.notifs, capturedNotify{Method: req.Method, Params: raw})
	} else {
		c.requests = append(c.requests, capturedRequest{Method: req.Method, Params: raw})
	}
	return nil, nil
}

func (c *clientStub) notifSnapshot() []capturedNotify {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]capturedNotify, len(c.notifs))
	copy(out, c.notifs)
	return out
}

func (c *clientStub) requestSnapshot() []capturedRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]capturedRequest, len(c.requests))
	copy(out, c.requests)
	return out
}

func newClientConnStub(ctx context.Context, stream io.ReadWriteCloser, c *clientStub) *jsonrpc2.Conn {
	return jsonrpc2.NewConn(
		ctx,
		jsonrpc2.NewBufferedStream(stream, jsonrpc2.VSCodeObjectCodec{}),
		jsonrpc2.HandlerWithError(c.handler),
	)
}

// writeWorkspaceFile creates dir + file and returns the absolute
// file path. Test helper for setting up a workspace tree.
func writeWorkspaceFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
	return path
}

func TestServer_InitializedWalksWorkspaceAndPublishes(t *testing.T) {
	// Workspace with a single invalid gaffer.toml. After
	// initialize+initialized the server should walk, parse, and
	// publish diagnostics for the file - without the client ever
	// sending didOpen.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	root := t.TempDir()
	cfg := writeWorkspaceFile(t, root, "gaffer.toml", `[[projection]]
name = "p"
entry = "p.js"
fixtures.evil = "../escape.json"
`)
	uri := pathToURI(cfg)

	_, done := startServerWithStore(ctx, srv, ServerOptions{})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{
		WorkspaceFolders: []WorkspaceFolder{{URI: pathToURI(root), Name: "ws"}},
	}, &InitializeResult{})
	_ = conn.Notify(ctx, MethodInitialized, struct{}{})

	waitFor(t, func() bool {
		got := findPublishDiagnostics(stub.notifSnapshot(), uri)
		return got != nil && len(got.Diagnostics) == 1
	}, time.Second)

	got := findPublishDiagnostics(stub.notifSnapshot(), uri)
	if got == nil || got.Diagnostics[0].Code != "fixture.path-escapes-root" {
		t.Fatalf("expected escape diagnostic, got %+v", got)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_InitializedRegistersFileWatcher(t *testing.T) {
	// Server should send a client/registerCapability request after
	// initialized, asking the editor to watch **/gaffer.toml.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	root := t.TempDir()
	_, done := startServerWithStore(ctx, srv, ServerOptions{})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{
		WorkspaceFolders: []WorkspaceFolder{{URI: pathToURI(root), Name: "ws"}},
	}, &InitializeResult{})
	_ = conn.Notify(ctx, MethodInitialized, struct{}{})

	waitFor(t, func() bool {
		for _, r := range stub.requestSnapshot() {
			if r.Method == MethodRegisterCapability {
				return true
			}
		}
		return false
	}, time.Second)

	var foundPattern string
	for _, r := range stub.requestSnapshot() {
		if r.Method != MethodRegisterCapability {
			continue
		}
		var p RegistrationParams
		if err := json.Unmarshal(r.Params, &p); err != nil {
			t.Fatalf("registerCapability params: %v", err)
		}
		if len(p.Registrations) == 0 {
			t.Fatalf("expected at least one registration: %+v", p)
		}
		reg := p.Registrations[0]
		if reg.Method != MethodDidChangeWatchedFiles {
			t.Errorf("registration method: got %q want %q", reg.Method, MethodDidChangeWatchedFiles)
		}
		// registerOptions arrives as a generic JSON object; round-trip
		// to extract the pattern.
		raw, _ := json.Marshal(reg.RegisterOptions)
		var opts DidChangeWatchedFilesRegistrationOptions
		if err := json.Unmarshal(raw, &opts); err != nil {
			t.Fatalf("registerOptions: %v", err)
		}
		if len(opts.Watchers) == 0 {
			t.Fatalf("expected at least one watcher: %+v", opts)
		}
		foundPattern = opts.Watchers[0].GlobPattern
	}
	if foundPattern != "**/gaffer.toml" {
		t.Errorf("watcher pattern: got %q want **/gaffer.toml", foundPattern)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_InitializedSkipsOpenBuffers(t *testing.T) {
	// A buffer the client opened during initialize must not be
	// overwritten by the walk's disk-sourced AddFromDisk - memory
	// wins. The store's Source must remain SourceMemory after the
	// walk completes.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	root := t.TempDir()
	cfg := writeWorkspaceFile(t, root, "gaffer.toml", `engine_version = 2`)
	uri := pathToURI(cfg)
	// Decoy gaffer.toml in a subdirectory. WalkConfigs returns paths
	// in lex order, so root/gaffer.toml is processed before
	// root/sub/gaffer.toml. Once we see diagnostics for the decoy
	// the walk has already passed the open buffer's URI.
	decoy := writeWorkspaceFile(t, filepath.Join(root, "sub"), "gaffer.toml", `[[projection]]
name = "p"
entry = "p.js"
fixtures.evil = "../escape.json"
`)
	decoyURI := pathToURI(decoy)

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{
		WorkspaceFolders: []WorkspaceFolder{{URI: pathToURI(root), Name: "ws"}},
	}, &InitializeResult{})
	// Open the buffer BEFORE initialized fires the walk. Memory
	// should win.
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, Text: "MEMORY-CONTENT"},
	})
	waitFor(t, func() bool {
		state, ok := server.docs.Get(uri)
		return ok && state.Source == SourceMemory
	}, time.Second)

	_ = conn.Notify(ctx, MethodInitialized, struct{}{})
	// Wait for the decoy's diagnostics - by then the walk has
	// already passed (and skipped) the open buffer's URI.
	waitFor(t, func() bool {
		got := findPublishDiagnostics(stub.notifSnapshot(), decoyURI)
		return got != nil
	}, time.Second)

	state, ok := server.docs.Get(uri)
	if !ok {
		t.Fatal("expected URI in store")
	}
	if state.Source != SourceMemory {
		t.Errorf("source: got %v want SourceMemory", state.Source)
	}
	if state.Content != "MEMORY-CONTENT" {
		t.Errorf("content: got %q want MEMORY-CONTENT", state.Content)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_DidChangeWatchedFiles_RereadsFromDisk(t *testing.T) {
	// Editor reports that the user edited gaffer.toml in another
	// editor / on the CLI. Server should re-read disk and republish.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	root := t.TempDir()
	cfg := writeWorkspaceFile(t, root, "gaffer.toml", `[[projection]]
name = "p"
entry = "p.js"
fixtures.evil = "../escape.json"
`)
	uri := pathToURI(cfg)

	_, done := startServerWithStore(ctx, srv, ServerOptions{})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{
		WorkspaceFolders: []WorkspaceFolder{{URI: pathToURI(root), Name: "ws"}},
	}, &InitializeResult{})
	_ = conn.Notify(ctx, MethodInitialized, struct{}{})
	waitFor(t, func() bool {
		got := findPublishDiagnostics(stub.notifSnapshot(), uri)
		return got != nil && len(got.Diagnostics) == 1
	}, time.Second)

	// Now repair the file on disk and tell the server it changed.
	if err := os.WriteFile(cfg, []byte(`[[projection]]
name = "p"
entry = "p.js"
fixtures.happy = "fixtures/happy.json"
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = conn.Notify(ctx, MethodDidChangeWatchedFiles, &DidChangeWatchedFilesParams{
		Changes: []FileEvent{{URI: uri, Type: FileChangeChanged}},
	})
	waitFor(t, func() bool {
		got := findPublishDiagnostics(stub.notifSnapshot(), uri)
		return got != nil && len(got.Diagnostics) == 0
	}, time.Second)

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_DidChangeWatchedFiles_DeletedClearsState(t *testing.T) {
	// Watcher reports the file was deleted. Server should drop it
	// from the store and clear its diagnostics.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	root := t.TempDir()
	cfg := writeWorkspaceFile(t, root, "gaffer.toml", `[[projection]]
name = "p"
entry = "p.js"
fixtures.evil = "../escape.json"
`)
	uri := pathToURI(cfg)

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{
		WorkspaceFolders: []WorkspaceFolder{{URI: pathToURI(root), Name: "ws"}},
	}, &InitializeResult{})
	_ = conn.Notify(ctx, MethodInitialized, struct{}{})
	waitFor(t, func() bool {
		_, ok := server.docs.Get(uri)
		return ok
	}, time.Second)

	_ = conn.Notify(ctx, MethodDidChangeWatchedFiles, &DidChangeWatchedFilesParams{
		Changes: []FileEvent{{URI: uri, Type: FileChangeDeleted}},
	})

	waitFor(t, func() bool {
		got := findPublishDiagnostics(stub.notifSnapshot(), uri)
		// Final published state is "no diagnostics" AND the URI is
		// gone from the store.
		_, ok := server.docs.Get(uri)
		return got != nil && len(got.Diagnostics) == 0 && !ok
	}, time.Second)

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_DidChangeWatchedFiles_CreatedSeedsAndPublishes(t *testing.T) {
	// Primary use case: a new gaffer.toml drops into the workspace
	// after the initial walk. Watcher reports Created; server reads
	// and publishes diagnostics.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	root := t.TempDir()
	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{
		WorkspaceFolders: []WorkspaceFolder{{URI: pathToURI(root), Name: "ws"}},
	}, &InitializeResult{})
	_ = conn.Notify(ctx, MethodInitialized, struct{}{})
	// Wait for the registerCapability handshake so we know the
	// initial walk finished (it ran zero files).
	waitFor(t, func() bool {
		for _, r := range stub.requestSnapshot() {
			if r.Method == MethodRegisterCapability {
				return true
			}
		}
		return false
	}, time.Second)

	// Drop a fresh file and tell the server.
	cfg := writeWorkspaceFile(t, root, "gaffer.toml", `[[projection]]
name = "p"
entry = "p.js"
fixtures.evil = "../escape.json"
`)
	uri := pathToURI(cfg)
	_ = conn.Notify(ctx, MethodDidChangeWatchedFiles, &DidChangeWatchedFilesParams{
		Changes: []FileEvent{{URI: uri, Type: FileChangeCreated}},
	})

	waitFor(t, func() bool {
		got := findPublishDiagnostics(stub.notifSnapshot(), uri)
		return got != nil && len(got.Diagnostics) == 1
	}, time.Second)
	if _, ok := server.docs.Get(uri); !ok {
		t.Fatal("expected URI in store after Created event")
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_DidChangeWatchedFiles_OpenBufferSurvivesDiskEvent(t *testing.T) {
	// User has gaffer.toml open with valid in-memory content. A
	// disk-side write happens (perhaps a teammate's git pull) and
	// the watcher reports Changed. The buffer must NOT be
	// overwritten - memory wins until the user closes the buffer.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	root := t.TempDir()
	cfg := writeWorkspaceFile(t, root, "gaffer.toml", `[[projection]]
name = "p"
entry = "p.js"
fixtures.evil = "../escape.json"
`)
	uri := pathToURI(cfg)

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{
		WorkspaceFolders: []WorkspaceFolder{{URI: pathToURI(root), Name: "ws"}},
	}, &InitializeResult{})
	memContent := `[[projection]]
name = "p"
entry = "p.js"
fixtures.happy = "fixtures/happy.json"
`
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, Text: memContent},
	})
	_ = conn.Notify(ctx, MethodInitialized, struct{}{})
	waitFor(t, func() bool {
		state, ok := server.docs.Get(uri)
		return ok && state.Source == SourceMemory && state.Content == memContent
	}, time.Second)

	// Disk-side change + watcher event.
	if err := os.WriteFile(cfg, []byte("CORRUPT-DISK-CONTENT"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = conn.Notify(ctx, MethodDidChangeWatchedFiles, &DidChangeWatchedFilesParams{
		Changes: []FileEvent{{URI: uri, Type: FileChangeChanged}},
	})
	// Give the handler time to (incorrectly) overwrite if it would.
	time.Sleep(100 * time.Millisecond)

	state, ok := server.docs.Get(uri)
	if !ok {
		t.Fatal("expected URI to remain in store")
	}
	if state.Source != SourceMemory {
		t.Errorf("source: got %v want SourceMemory", state.Source)
	}
	if state.Content != memContent {
		t.Errorf("buffer was overwritten: got %q", state.Content)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_InitializeWithoutInitializedDoesNotWalk(t *testing.T) {
	// Pin the lifecycle: the workspace walk and watcher
	// registration only fire after `initialized`. If the client
	// stops at `initialize`, the server must not poke the
	// filesystem or push capability registrations.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	root := t.TempDir()
	cfg := writeWorkspaceFile(t, root, "gaffer.toml", `[[projection]]
name = "p"
entry = "p.js"
fixtures.evil = "../escape.json"
`)
	uri := pathToURI(cfg)
	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{
		WorkspaceFolders: []WorkspaceFolder{{URI: pathToURI(root), Name: "ws"}},
	}, &InitializeResult{})

	// Wait long enough for any errant goroutine to have done its
	// work. Negative assertion - polling can't help us here.
	time.Sleep(150 * time.Millisecond)
	if _, ok := server.docs.Get(uri); ok {
		t.Errorf("expected URI absent from store after initialize-only")
	}
	for _, r := range stub.requestSnapshot() {
		if r.Method == MethodRegisterCapability {
			t.Errorf("registerCapability fired without initialized")
		}
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_DidChangeWatchedFiles_NonGafferIgnored(t *testing.T) {
	// Editors sometimes register broader watchers; ensure the
	// gate by basename keeps non-gaffer events from reaching the
	// document store.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{})
	_ = conn.Notify(ctx, MethodInitialized, struct{}{})

	uri := "file:///workspace/projection.js"
	_ = conn.Notify(ctx, MethodDidChangeWatchedFiles, &DidChangeWatchedFilesParams{
		Changes: []FileEvent{{URI: uri, Type: FileChangeChanged}},
	})
	// Give the handler a moment to (not) act.
	time.Sleep(100 * time.Millisecond)
	if _, ok := server.docs.Get(uri); ok {
		t.Errorf("expected non-gaffer URI to be ignored, found in store")
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}
