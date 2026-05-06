package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
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

	docs      *documentStore
	debouncer *debouncer

	mu          sync.Mutex
	conn        *jsonrpc2.Conn // captured during Run, used for server-pushed notifications
	initialized bool
	shutdownReq bool
	// draining flips true once Run's defer starts winding down.
	// spawn() checks this under mu before incrementing wg; without
	// the gate, a handler racing teardown could call wg.Add(1)
	// after wg.Wait had already returned, which is a documented
	// data race for sync.WaitGroup.
	draining bool
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
	// codeLensRefreshSupported mirrors the client's
	// workspace.codeLens.refreshSupport capability so we don't
	// fire workspace/codeLens/refresh into a void. LSP 3.16 spec:
	// servers MUST gate the request on this.
	codeLensRefreshSupported bool
	// exitCh closes when the client sends `exit`. Run selects on
	// this so the server tears down its connection without waiting
	// for the client to also close stdin (a well-behaved client
	// expects the server to drive disconnect on exit).
	exitCh chan struct{}

	// wg tracks goroutines spawned from handlers (parse-and-
	// publish, the initialized walk, watched-file event batches)
	// so Run can wait for them to drain before returning.
	wg sync.WaitGroup
}

// NewServer constructs a server with the given options. Doesn't
// touch I/O; call Run to start the message loop.
func NewServer(opts ServerOptions) *Server {
	window := opts.DebounceWindow
	if window <= 0 {
		window = defaultDebounceWindow
	}
	return &Server{
		opts:      opts,
		docs:      newDocumentStore(),
		debouncer: newDebouncer(window),
		exitCh:    make(chan struct{}),
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
		// Teardown order:
		//   1. Cancel runCtx so in-flight parseAndPublish notices
		//      and exits its DescribeBytes call promptly.
		//   2. Drain debouncer timers - any already-queued
		//      callback's identity check finds an empty map and
		//      bails.
		//   3. Set draining under mu - spawn() now refuses new
		//      work, so wg.Add can no longer race wg.Wait.
		//   4. Wait for already-spawned goroutines to settle.
		//   5. Clear captured fields. Goroutines that ran during
		//      steps 1-4 saw the still-live conn/runCtx, which is
		//      what the wg promise gives them.
		cancel()
		s.debouncer.drain()
		s.mu.Lock()
		s.draining = true
		s.mu.Unlock()
		s.wg.Wait()
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
	var ctxQuit bool
	select {
	case <-conn.DisconnectNotify():
	case <-s.exitCh:
		_ = conn.Close()
		<-conn.DisconnectNotify()
	case <-ctx.Done():
		ctxQuit = true
		_ = conn.Close()
		<-conn.DisconnectNotify()
	}
	if ctxQuit {
		// Server-side quit (caller cancelled ctx, e.g. SIGINT).
		// Don't blame the client for not sending shutdown - they
		// had no chance.
		return nil
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
		_, runCtx := s.snapshotRunState()
		if runCtx != nil {
			s.spawn(func() { s.handleInitialized(runCtx) })
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
	case MethodWorkspaceSymbol:
		return s.handleWorkspaceSymbol(req)
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
	s.codeLensRefreshSupported = false
	if req.Params != nil {
		params, jerr := decodeParams[InitializeParams](req, "initialize")
		if jerr != nil {
			return nil, jerr
		}
		s.roots = extractRoots(params)
		if cl := params.Capabilities.Workspace.CodeLens; cl != nil && cl.RefreshSupport {
			s.codeLensRefreshSupported = true
		}
	}
	s.initialized = true
	return InitializeResult{
		Capabilities: ServerCapabilities{
			TextDocumentSync:        1, // full document sync (Decision 1)
			CodeLensProvider:        &CodeLensOptions{},
			WorkspaceSymbolProvider: &WorkspaceSymbolOptions{},
		},
		ServerInfo: ServerInfo{
			Name:    "gaffer-lsp",
			Version: s.opts.Version,
		},
	}, nil
}

func (s *Server) handleDidOpen(_ context.Context, req *jsonrpc2.Request) (interface{}, error) {
	params, jerr := decodeParams[DidOpenTextDocumentParams](req, "didOpen")
	if jerr != nil {
		return nil, jerr
	}
	s.docs.Open(params.TextDocument.URI, params.TextDocument.Text)
	// didOpen drives the first parse - users expect immediate
	// feedback when a file opens, not a 250ms wait. Cancel any
	// stale debounce from a previous Open/Change cycle so two
	// parses don't race.
	s.debouncer.cancel(params.TextDocument.URI)
	_, runCtx := s.snapshotRunState()
	if runCtx == nil {
		return nil, nil
	}
	uri := params.TextDocument.URI
	s.spawn(func() { s.parseAndPublish(runCtx, uri) })
	return nil, nil
}

func (s *Server) handleDidChange(_ context.Context, req *jsonrpc2.Request) (interface{}, error) {
	params, jerr := decodeParams[DidChangeTextDocumentParams](req, "didChange")
	if jerr != nil {
		return nil, jerr
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
	_, runCtx := s.snapshotRunState()
	if runCtx == nil {
		return nil, nil
	}
	uri := params.TextDocument.URI
	// The debounce callback is intentionally NOT tracked by s.wg.
	// AfterFunc runs the callback in a goroutine the debouncer
	// owns; tracking it would force schedule to .Add(1) and the
	// callback to .Done(), but a callback that bails on its own
	// identity check (because schedule cancelled it) would never
	// run Done. Late callbacks instead rely on runCtx
	// cancellation - parseAndPublish returns promptly when ctx is
	// done, so the wg drain doesn't need to wait on them.
	s.debouncer.schedule(uri, func() {
		s.parseAndPublish(runCtx, uri)
	})
	return nil, nil
}

func (s *Server) handleDidClose(req *jsonrpc2.Request) (interface{}, error) {
	params, jerr := decodeParams[DidCloseTextDocumentParams](req, "didClose")
	if jerr != nil {
		return nil, jerr
	}
	// Cancel any pending debounce - parsing a closed buffer would
	// publish stale squiggles that the close itself is meant to
	// clear.
	s.debouncer.cancel(params.TextDocument.URI)
	hadParse := false
	if _, ok := s.docs.GetParse(params.TextDocument.URI); ok {
		hadParse = true
	}
	s.docs.Close(params.TextDocument.URI)
	// Clear any lingering diagnostics for this URI so the editor's
	// Problems panel doesn't show squiggles for a file that's no
	// longer open.
	s.publishDiagnostics(params.TextDocument.URI, []lspDiagnostic{})
	if hadParse {
		// A cached parse just went away; any .js URI whose
		// lenses depended on its projections is stale.
		s.requestCodeLensRefresh()
	}
	return nil, nil
}

func (s *Server) handleCodeLens(req *jsonrpc2.Request) (interface{}, error) {
	if req.Params == nil {
		return []CodeLens{}, nil
	}
	params, jerr := decodeParams[CodeLensParams](req, "codeLens")
	if jerr != nil {
		return nil, jerr
	}
	uri := params.TextDocument.URI
	// gaffer.toml URIs serve from their own cached parse. Any
	// other URI (typically a projection entry .js) serves by
	// scanning every cached parse for a matching entry path.
	if isGafferConfig(uri) {
		parse, ok := s.docs.GetParse(uri)
		if !ok {
			return []CodeLens{}, nil
		}
		return emitCodeLenses(parse.Description, uri), nil
	}
	return emitEntryScriptLenses(s.docs.AllParses(), uri), nil
}

func (s *Server) handleWorkspaceSymbol(req *jsonrpc2.Request) (interface{}, error) {
	// Empty params is a legal "give me everything" query. Our
	// catalogue (one entry per projection) is small enough that
	// the client can do its own filtering on the result; we
	// always return the full set.
	if req.Params != nil {
		if _, jerr := decodeParams[WorkspaceSymbolParams](req, "workspace/symbol"); jerr != nil {
			return nil, jerr
		}
	}
	return emitWorkspaceSymbols(s.docs.AllParses()), nil
}

// extractRoots returns workspace folder paths in WorkspaceFolders
// (preferred per the LSP spec) or, for older clients, falls back
// to RootURI. Non-file URIs (vscode-vfs://, untitled:, etc.) are
// dropped - the workspace walker reads from the local filesystem,
// so a remote-workspace URI would surface as a "permission denied"
// log line. Empty slice for single-buffer sessions without any
// workspace.
func extractRoots(params InitializeParams) []string {
	var roots []string
	add := func(uri string) {
		if !strings.HasPrefix(uri, "file://") {
			return
		}
		if path := uriToPath(uri); path != "" {
			roots = append(roots, path)
		}
	}
	if len(params.WorkspaceFolders) > 0 {
		for _, wf := range params.WorkspaceFolders {
			add(wf.URI)
		}
		return roots
	}
	if params.RootURI != "" {
		add(params.RootURI)
	}
	return roots
}

// snapshotRunState atomically returns the conn and runCtx
// captured by the active Run. Both are nil after Run has begun
// teardown. Handlers that need to spawn work must check before
// using the result.
func (s *Server) snapshotRunState() (*jsonrpc2.Conn, context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn, s.runCtx
}

// spawn runs fn in a goroutine tracked by s.wg so Run's defer
// can wait for it to drain before clearing captured fields.
// Returns false (and does not run fn) if Run has begun winding
// down - no-op for callers, since the runCtx they captured is
// already cancelled and any work would terminate immediately.
//
// The draining check + wg.Add(1) MUST happen under s.mu.
// Otherwise a handler could pass snapshotRunState (getting non-
// nil conn/ctx) and call spawn after Run's defer began wg.Wait
// with counter zero - the resulting Add races with Wait.
func (s *Server) spawn(fn func()) bool {
	s.mu.Lock()
	if s.draining {
		s.mu.Unlock()
		return false
	}
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		fn()
	}()
	return true
}

// decodeParams is the shared boilerplate for unmarshalling a
// jsonrpc2 request's params into a typed payload. label
// identifies the method for the missing-params error message.
func decodeParams[T any](req *jsonrpc2.Request, label string) (T, *jsonrpc2.Error) {
	var out T
	if req.Params == nil {
		return out, &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidParams,
			Message: label + " missing params",
		}
	}
	if err := json.Unmarshal(*req.Params, &out); err != nil {
		return out, &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidParams,
			Message: err.Error(),
		}
	}
	return out, nil
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
