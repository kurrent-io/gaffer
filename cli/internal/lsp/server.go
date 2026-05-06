package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/sourcegraph/jsonrpc2"
)

// defaultDebounceWindow is the canonical LSP "pause to read"
// interval - long enough that transient invalid states during a
// keystroke burst (`fixtures.foo = "` -> `"esc` -> `"escape.json"`)
// don't flicker squiggles, short enough that the user perceives
// feedback as live.
const defaultDebounceWindow = 250 * time.Millisecond

// ServerOptions configures a Server before Run starts the message
// loop. Zero-value is usable; callers override individual fields.
type ServerOptions struct {
	// Version surfaced via InitializeResult.ServerInfo.Version.
	// Callers (e.g. cmd/lsp.go) inject the build version.
	Version string
	// DebounceWindow gates how long a quiet period must follow a
	// didChange before the server runs the parse + publish for that
	// URI. Each new didChange resets the window. Zero falls back to
	// the conventional 250ms (matches gopls/clangd/pyright).
	DebounceWindow time.Duration
}

// Server is the gaffer LSP server. One instance per stdio session;
// the message loop runs in Run and exits when the client closes
// stdin, sends shutdown+exit, or the run context is cancelled.
//
// Concurrency: jsonrpc2 dispatches each request in its own
// goroutine, so handler methods are called concurrently. The
// document store has its own mutex; lifecycle flags here are
// guarded by mu.
type Server struct {
	opts ServerOptions

	docs *DocumentStore

	mu          sync.Mutex
	conn        *jsonrpc2.Conn // captured during Run, used for server-pushed notifications
	initialized bool
	shutdownReq bool
	// runCtx is cancelled when Run is about to return (clean exit,
	// disconnect, or ctx-Done). Long-running work spawned from
	// handlers - the workspace walk, watched-file event processing -
	// derive from this so shutdown doesn't leave goroutines blocked
	// on I/O after the connection is gone.
	runCtx    context.Context
	cancelRun context.CancelFunc
	// roots holds workspace folder paths captured from initialize.
	// Used by the initialized handler to walk for gaffer.toml files.
	// Stored as filesystem paths (URIs converted at capture time)
	// so the walker doesn't need to re-do the conversion.
	roots []string
	// exitCh closes when the client sends `exit`. Run selects on
	// this so the server tears down its connection without waiting
	// for the client to also close stdin (a well-behaved client
	// expects the server to drive disconnect on exit).
	exitCh chan struct{}

	// debounceMu guards the per-URI debounce timer map. Held only
	// for the timer-table mutation, not while the timer's callback
	// runs - that path takes the lock again to delete its own
	// entry.
	debounceMu sync.Mutex
	debounces  map[string]*time.Timer
}

// NewServer constructs a server with the given options. Doesn't
// touch I/O; call Run to start the message loop.
func NewServer(opts ServerOptions) *Server {
	return &Server{
		opts:      opts,
		docs:      NewDocumentStore(),
		exitCh:    make(chan struct{}),
		debounces: make(map[string]*time.Timer),
	}
}

// debounceWindow returns the configured window or the default.
// Centralised so future per-method overrides land in one place.
func (s *Server) debounceWindow() time.Duration {
	if s.opts.DebounceWindow > 0 {
		return s.opts.DebounceWindow
	}
	return defaultDebounceWindow
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
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	conn := jsonrpc2.NewConn(
		runCtx,
		jsonrpc2.NewBufferedStream(stream, jsonrpc2.VSCodeObjectCodec{}),
		jsonrpc2.HandlerWithError(s.handle),
	)
	// Capture the conn + runCtx for server-pushed notifications
	// (publishDiagnostics, registerCapability) and for spawned work
	// (workspace walk) that needs a shutdown signal. Cleared on
	// disconnect so handlers that reach for them post-shutdown bail
	// cleanly.
	s.mu.Lock()
	s.conn = conn
	s.runCtx = runCtx
	s.cancelRun = cancel
	s.mu.Unlock()
	defer func() {
		// Cancel runCtx first so any in-flight parseAndPublish
		// notices and exits its DescribeBytes call promptly. Then
		// drain pending timers - any already-queued callback's
		// identity check finds an empty map and bails. Only
		// after both have happened do we clear the captured
		// fields, so a late callback's runContext() lookup still
		// returns the cancelled ctx (rather than falling back to
		// Background and running uncancellable work).
		cancel()
		s.drainDebounces()
		s.mu.Lock()
		s.conn = nil
		s.runCtx = nil
		s.cancelRun = nil
		s.mu.Unlock()
	}()
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
		// Notification. Now the client is ready to receive
		// server-pushed messages, kick off the workspace walk and
		// register the watcher pattern. Uses runCtx so shutdown
		// cancels any in-flight walk.
		s.mu.Lock()
		runCtx := s.runCtx
		s.mu.Unlock()
		if runCtx != nil {
			go s.handleInitialized(runCtx)
		}
		return nil, nil
	case MethodShutdown:
		return s.handleShutdown()
	case MethodExit:
		// Notification. Signal Run to tear down the connection -
		// LSP spec expects the server to terminate on exit, not
		// hang waiting for the client to also close stdin.
		s.signalExit()
		return nil, nil
	case MethodDidOpen:
		return s.handleDidOpen(ctx, req)
	case MethodDidChange:
		return s.handleDidChange(ctx, req)
	case MethodDidClose:
		return s.handleDidClose(req)
	case MethodDidSave:
		// No-op for V1: under full sync we already have the latest
		// content from didChange. Acknowledge and move on.
		return nil, nil
	case MethodCodeLens:
		return s.handleCodeLens(req)
	case MethodDidChangeWatchedFiles:
		return s.handleDidChangeWatchedFiles(ctx, req)
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
	// Capture workspace roots for the initialized-time walk.
	// WorkspaceFolders supersedes RootURI per the LSP spec; fall
	// back to RootURI only when the modern field is absent (older
	// clients, or callers that haven't set it). Empty params is
	// legal - the server still works for single-buffer sessions
	// without any workspace.
	s.roots = nil
	if req.Params != nil {
		var params InitializeParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return nil, &jsonrpc2.Error{
				Code:    jsonrpc2.CodeInvalidParams,
				Message: err.Error(),
			}
		}
		if len(params.WorkspaceFolders) > 0 {
			for _, wf := range params.WorkspaceFolders {
				if path := uriToPath(wf.URI); path != "" {
					s.roots = append(s.roots, path)
				}
			}
		} else if params.RootURI != "" {
			if path := uriToPath(params.RootURI); path != "" {
				s.roots = append(s.roots, path)
			}
		}
	}
	s.initialized = true
	return InitializeResult{
		Capabilities: ServerCapabilities{
			TextDocumentSync: 1, // full document sync (Decision 1)
			CodeLensProvider: &CodeLensOptions{},
		},
		ServerInfo: ServerInfo{
			Name:    "gaffer-lsp",
			Version: s.opts.Version,
		},
	}, nil
}

func (s *Server) handleDidOpen(_ context.Context, req *jsonrpc2.Request) (interface{}, error) {
	if req.Params == nil {
		return nil, &jsonrpc2.Error{Code: jsonrpc2.CodeInvalidParams, Message: "didOpen missing params"}
	}
	var params DidOpenTextDocumentParams
	if err := json.Unmarshal(*req.Params, &params); err != nil {
		return nil, &jsonrpc2.Error{Code: jsonrpc2.CodeInvalidParams, Message: err.Error()}
	}
	s.docs.Open(params.TextDocument.URI, params.TextDocument.Text)
	// didOpen drives the first parse - users expect immediate
	// feedback when a file opens, not a 250ms wait. Cancel any
	// stale debounce from a previous Open/Change cycle so two
	// parses don't race.
	s.cancelDebounce(params.TextDocument.URI)
	go s.parseAndPublish(s.runContext(), params.TextDocument.URI)
	return nil, nil
}

func (s *Server) handleDidChange(_ context.Context, req *jsonrpc2.Request) (interface{}, error) {
	if req.Params == nil {
		return nil, &jsonrpc2.Error{Code: jsonrpc2.CodeInvalidParams, Message: "didChange missing params"}
	}
	var params DidChangeTextDocumentParams
	if err := json.Unmarshal(*req.Params, &params); err != nil {
		return nil, &jsonrpc2.Error{Code: jsonrpc2.CodeInvalidParams, Message: err.Error()}
	}
	if len(params.ContentChanges) == 0 {
		// Spec allows this but it's a no-op for full sync.
		return nil, nil
	}
	// Full sync: take the last change's text as authoritative.
	// (Spec says clients SHOULD send only one event under full
	// sync, but be liberal in what we accept.)
	last := params.ContentChanges[len(params.ContentChanges)-1]
	if _, err := s.docs.Change(params.TextDocument.URI, last.Text); err != nil {
		// Client bug (didChange before didOpen, or against a
		// disk-sourced URI). Log to stderr so a misbehaving client
		// is observable; returning nil keeps the connection alive
		// since notifications can't carry an error response back
		// to the client anyway.
		log.Printf("lsp: dropping didChange: %v", err)
		return nil, nil
	}
	s.scheduleDebouncedParse(params.TextDocument.URI)
	return nil, nil
}

func (s *Server) handleDidClose(req *jsonrpc2.Request) (interface{}, error) {
	if req.Params == nil {
		return nil, &jsonrpc2.Error{Code: jsonrpc2.CodeInvalidParams, Message: "didClose missing params"}
	}
	var params DidCloseTextDocumentParams
	if err := json.Unmarshal(*req.Params, &params); err != nil {
		return nil, &jsonrpc2.Error{Code: jsonrpc2.CodeInvalidParams, Message: err.Error()}
	}
	// Cancel any pending debounce - parsing a closed buffer would
	// publish stale squiggles that the close itself is meant to
	// clear.
	s.cancelDebounce(params.TextDocument.URI)
	s.docs.Close(params.TextDocument.URI)
	// Clear any lingering diagnostics for this URI so the editor's
	// Problems panel doesn't show squiggles for a file that's no
	// longer open.
	s.publishDiagnostics(params.TextDocument.URI, []LSPDiagnostic{})
	return nil, nil
}

func (s *Server) handleCodeLens(req *jsonrpc2.Request) (interface{}, error) {
	if req.Params == nil {
		return []CodeLens{}, nil
	}
	var params CodeLensParams
	if err := json.Unmarshal(*req.Params, &params); err != nil {
		return nil, &jsonrpc2.Error{Code: jsonrpc2.CodeInvalidParams, Message: err.Error()}
	}
	parse, ok := s.docs.GetParse(params.TextDocument.URI)
	if !ok {
		// No parse cached - either didOpen hasn't happened or the
		// URI isn't a gaffer config. Empty response is the
		// canonical "no lenses for this document."
		return []CodeLens{}, nil
	}
	return emitCodeLenses(parse.Description), nil
}

// runContext returns the captured Run-scope context. Used by
// async work spawned from per-request handlers so shutdown
// cancels their parse/publish in flight. Falls back to
// context.Background when called outside an active Run (during
// tests that drive handlers directly).
func (s *Server) runContext() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runCtx != nil {
		return s.runCtx
	}
	return context.Background()
}

// scheduleDebouncedParse arms (or re-arms) the per-URI debounce
// timer. The most recent didChange wins: each call cancels the
// pending timer and replaces it with a fresh one. After the
// window elapses with no further didChange the parse runs against
// the current document store state.
//
// Callback identity check: AfterFunc's Stop() doesn't wait for an
// already-fired callback. A callback that lost the Stop race
// could otherwise corrupt the map (deleting a successor's entry)
// or run a parse the caller meant to cancel. Each callback checks
// "am I still the current timer for this URI?" before doing
// anything; if a later scheduleDebouncedParse / cancelDebounce /
// drainDebounces replaced or removed our entry, the callback is
// stale and bails.
func (s *Server) scheduleDebouncedParse(uri string) {
	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()
	if t, ok := s.debounces[uri]; ok {
		t.Stop()
	}
	var timer *time.Timer
	timer = time.AfterFunc(s.debounceWindow(), func() {
		s.debounceMu.Lock()
		if s.debounces[uri] != timer {
			s.debounceMu.Unlock()
			return
		}
		delete(s.debounces, uri)
		s.debounceMu.Unlock()
		s.parseAndPublish(s.runContext(), uri)
	})
	s.debounces[uri] = timer
}

// cancelDebounce stops the pending debounce timer for URI and
// removes it from the map. The map removal alone is sufficient -
// even if the callback has already been queued, the identity
// check inside it will bail.
func (s *Server) cancelDebounce(uri string) {
	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()
	if t, ok := s.debounces[uri]; ok {
		t.Stop()
		delete(s.debounces, uri)
	}
}

// drainDebounces stops every pending timer at shutdown. Map
// entries are deleted so any already-queued callbacks fail their
// identity check and bail without calling parseAndPublish.
func (s *Server) drainDebounces() {
	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()
	for uri, t := range s.debounces {
		t.Stop()
		delete(s.debounces, uri)
	}
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
