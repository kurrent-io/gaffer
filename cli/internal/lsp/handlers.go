package lsp

import (
	"context"
	"encoding/json"
	"log"
	"strings"

	"github.com/sourcegraph/jsonrpc2"
)

// offloadBlockingHandler runs blocking request handlers in their own goroutine
// so they don't freeze the single read-loop goroutine that serves all other LSP
// traffic. Non-blocking methods stay inline, preserving the in-order processing
// document sync relies on. Unlike jsonrpc2.AsyncHandler - which offloads every
// message and so reorders notifications - this offloads only the methods that
// need it, and reuses the wrapped handler's reply machinery.
//
// The offloaded work goes through spawn, so it's tracked by the server's wait
// group and drained at teardown like every other async path - not a bare
// goroutine that Run could abandon mid-read.
type offloadBlockingHandler struct {
	inner jsonrpc2.Handler
	spawn func(func()) bool
}

func offloadBlocking(inner jsonrpc2.Handler, spawn func(func()) bool) jsonrpc2.Handler {
	return offloadBlockingHandler{inner: inner, spawn: spawn}
}

func (h offloadBlockingHandler) Handle(ctx context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) {
	if !req.Notif && blocksReadLoop(req.Method) {
		// spawn returns false only once Run is draining; then the ctx is already
		// cancelled, so running inline replies at once without a real read.
		if h.spawn(func() { h.inner.Handle(ctx, conn, req) }) {
			return
		}
	}
	h.inner.Handle(ctx, conn, req)
}

// blockingMethods is the set of request methods whose handler does blocking I/O
// over the env connection (a bounded network read/write), so it must run off the
// read loop or it freezes all other LSP traffic for up to RPCTimeout against a
// slow or unreachable env - a freeze a fast-env test won't catch. Every such
// method MUST be listed here AND dispatched from handle's switch; a guard test
// (TestBlockingMethodsAreDispatched) checks the two don't drift. Everything else
// is a cache read or spawns its own work and returns at once.
var blockingMethods = map[string]struct{}{
	MethodDiffProjection:    {},
	MethodOperateProjection: {},
}

func blocksReadLoop(method string) bool {
	_, ok := blockingMethods[method]
	return ok
}

// handle dispatches a single JSON-RPC message to the right method.
// jsonrpc2.HandlerWithError takes care of error/result wrapping.
func (s *Server) handle(ctx context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (any, error) {
	// Wait for Run to finish storing the run state (see Server.ready):
	// dispatch can start before Run's conn assignment, and a handler
	// observing the half-initialized state loses work silently. Closed
	// microseconds after the conn exists; nil only when no Run is
	// active, which a dispatched handler can't observe.
	s.mu.Lock()
	ready := s.ready
	s.mu.Unlock()
	if ready != nil {
		<-ready
	}
	switch req.Method {
	case MethodInitialize:
		return s.handleInitialize(ctx, req)
	case MethodInitialized:
		// Notification. Now the client is ready to receive
		// server-pushed messages, kick off the workspace walk and
		// register the watcher pattern. spawnWithCtx is a no-op
		// if Run already wound down.
		s.spawnWithCtx(s.handleInitialized)
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
		return s.handleDidSave(req)
	case MethodCodeLens:
		return s.handleCodeLens(req)
	case MethodHover:
		return s.handleHover(req)
	case MethodWorkspaceSymbol:
		return s.handleWorkspaceSymbol(req)
	case MethodDidChangeWatchedFiles:
		return s.handleDidChangeWatchedFiles(ctx, req)
	case MethodProjectionDetails:
		return s.handleProjectionDetails(req)
	case MethodDiffProjection:
		// Blocking network read - must also be in blockingMethods, or it
		// runs inline and freezes the read loop.
		return s.handleDiffProjection(ctx, req)
	case MethodOperateProjection:
		// Blocking network write - must also be in blockingMethods.
		return s.handleOperateProjection(ctx, req)
	case MethodRefreshStatus:
		return s.handleRefreshStatus(req)
	default:
		// $/-prefixed messages are optional per the LSP spec.
		// Notifications must be silently ignored; requests get the
		// standard MethodNotFound response. Without this branch the
		// client's chatty $/setTrace pings flood the server log.
		if strings.HasPrefix(req.Method, "$/") && req.Notif {
			return nil, nil
		}
		// CodeMethodNotFound is dropped by jsonrpc2 when the
		// inbound was a notification (no ID, no response slot).
		// For requests it surfaces as a proper JSON-RPC error.
		return nil, &jsonrpc2.Error{
			Code:    jsonrpc2.CodeMethodNotFound,
			Message: "method not implemented: " + req.Method,
		}
	}
}

func (s *Server) handleInitialize(_ context.Context, req *jsonrpc2.Request) (any, error) {
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
	s.statusLensCapable = false
	if req.Params != nil {
		params, jerr := decodeParams[InitializeParams](req, "initialize")
		if jerr != nil {
			return nil, jerr
		}
		s.roots = extractRoots(params)
		if cl := params.Capabilities.Workspace.CodeLens; cl != nil && cl.RefreshSupport {
			s.codeLensRefreshSupported = true
		}
		if len(params.InitOptions) > 0 {
			var opts InitializationOptions
			// Best-effort: a malformed blob just leaves status disabled.
			if err := json.Unmarshal(params.InitOptions, &opts); err == nil {
				s.statusLensCapable = opts.StatusLens
			}
		}
	}
	s.initialized = true
	caps := ServerCapabilities{
		TextDocumentSync: TextDocumentSyncOptions{
			OpenClose: true,
			Change:    TextDocumentSyncFull,
			Save:      true,
		},
		CodeLensProvider:        &CodeLensOptions{},
		WorkspaceSymbolProvider: &WorkspaceSymbolOptions{},
	}
	// Hover serves per-projection deploy status, part of the opt-in status
	// surface - advertise it only to a client that asked for that surface, so
	// editors without it keep their own hover behaviour.
	if s.statusLensCapable {
		caps.HoverProvider = &HoverOptions{}
	}
	return InitializeResult{
		Capabilities: caps,
		ServerInfo: ServerInfo{
			Name:    "gaffer-lsp",
			Version: s.opts.Version,
		},
	}, nil
}

func (s *Server) handleDidOpen(_ context.Context, req *jsonrpc2.Request) (any, error) {
	params, jerr := decodeParams[DidOpenTextDocumentParams](req, "didOpen")
	if jerr != nil {
		return nil, jerr
	}
	s.docs.Open(params.TextDocument.URI, params.TextDocument.Text)
	// didOpen drives the first parse - users expect immediate
	// feedback when a file opens, not a debounce-window wait.
	s.triggerParse(params.TextDocument.URI, true)
	// Kick a deploy-status read for the opened config so the env surface renders
	// live state; recompute drift since we have nothing cached. No-op for
	// non-config URIs.
	s.refreshStatus(params.TextDocument.URI, true)
	return nil, nil
}

func (s *Server) handleDidChange(_ context.Context, req *jsonrpc2.Request) (any, error) {
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
	// didChange debounces - rapid keystrokes collapse to one parse
	// per quiet window.
	s.triggerParse(params.TextDocument.URI, false)
	return nil, nil
}

// triggerParse drives a parse-and-publish for uri, either immediately
// (didOpen, on file open) or on the debouncer (didChange, coalescing
// rapid keystrokes). The immediate path also cancels any pending
// debounce so an in-flight keystroke timer can't race the open parse.
// Both paths bind the work to the active Run's runCtx and become a
// no-op once Run has wound down.
//
// The debounced callback is intentionally NOT tracked by s.wg.
// AfterFunc runs it in a goroutine the debouncer owns; tracking it
// would force schedule to .Add(1) and the callback to .Done(), but a
// callback that bails on its own identity check (because a later
// schedule cancelled it) would never run Done. Late callbacks instead
// rely on runCtx cancellation - parseAndPublish returns promptly when
// ctx is done, so the wg drain doesn't need to wait on them. The
// immediate path goes through spawnWithCtx, which IS wg-tracked.
func (s *Server) triggerParse(uri string, immediate bool) {
	if immediate {
		// Cancel any stale debounce from a previous Open/Change cycle
		// so two parses don't race.
		s.debouncer.cancel(uri)
		s.spawnWithCtx(func(runCtx context.Context) {
			s.parseAndPublish(runCtx, uri)
		})
		return
	}
	_, runCtx := s.snapshotRunState()
	if runCtx == nil {
		return
	}
	s.debouncer.schedule(uri, func() {
		s.parseAndPublish(runCtx, uri)
	})
}

func (s *Server) handleDidClose(req *jsonrpc2.Request) (any, error) {
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
	// Drop cached deploy status and stop its definition-stream subscriptions -
	// the surface is gone with the buffer.
	s.statusCache.drop(params.TextDocument.URI)
	s.stopWatches(params.TextDocument.URI)
	// Fire the refresh BEFORE publishDiagnostics. publishDiagnostics
	// is a synchronous conn.Notify holding the conn write lock for
	// the duration of the wire write; if it blocks (slow client, or
	// a concurrent server-side conn.Call holding the lock),
	// requestCodeLensRefresh would be gated on it. Refresh is
	// fire-and-forget on a separate goroutine, so spawning it first
	// keeps the .js entry-script lens invalidation off the
	// publishDiagnostics critical path.
	if hadParse {
		// A cached parse just went away; any .js URI whose
		// lenses depended on its projections is stale.
		s.requestCodeLensRefresh()
	}
	// Clear any lingering diagnostics for this URI so the editor's
	// Problems panel doesn't show squiggles for a file that's no
	// longer open.
	s.publishDiagnostics(params.TextDocument.URI, []lspDiagnostic{})
	return nil, nil
}

func (s *Server) handleCodeLens(req *jsonrpc2.Request) (any, error) {
	s.stats.codeLensRequests.Add(1)
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
		lenses := emitCodeLenses(parse.Description, uri)
		// Status lenses only go to a client that opted in (see statusLensCapable):
		// the informational roll-up isn't a routable command a generic client
		// could render sanely, so we don't emit it to one that can't.
		if s.statusLensEnabled() {
			statuses := s.statusCache.get(uri)
			loading := s.statusCache.inFlightEnvs(uri)
			lenses = append(lenses, emitStatusEnvLenses(parse.Description, uri, statuses, loading)...)
			lenses = append(lenses, emitStatusBadgeLenses(parse.Description, statuses, loading)...)
			lenses = append(lenses, emitActionsLenses(parse.Description, uri)...)
		}
		return lenses, nil
	}
	return emitEntryScriptLenses(s.docs.AllParses(), uri), nil
}

// handleHover serves a per-projection deploy-status tooltip on a [[projection]]
// header: a table of every configured env's runtime state and drift verdict,
// read from the same status cache the env-block lenses use. Returns nil (no
// hover) when the client didn't opt into the status surface, the URI isn't a
// gaffer.toml, or the cursor isn't on a projection header.
func (s *Server) handleHover(req *jsonrpc2.Request) (any, error) {
	if !s.statusLensEnabled() {
		return nil, nil
	}
	if req.Params == nil {
		return nil, nil
	}
	params, jerr := decodeParams[HoverParams](req, "hover")
	if jerr != nil {
		return nil, jerr
	}
	uri := params.TextDocument.URI
	if !isGafferConfig(uri) {
		return nil, nil
	}
	parse, ok := s.docs.GetParse(uri)
	if !ok {
		return nil, nil
	}
	proj, ok := projectionAt(parse.Description, params.Position.Line)
	if !ok {
		return nil, nil
	}
	md := projectionHoverMarkdown(parse.Description, proj, s.statusCache.get(uri), s.statusCache.inFlightEnvs(uri))
	if md == "" {
		return nil, nil
	}
	r := rangeToLSP(proj.Range)
	return Hover{
		Contents: MarkupContent{Kind: MarkupKindMarkdown, Value: md},
		Range:    &r,
	}, nil
}

func (s *Server) handleWorkspaceSymbol(req *jsonrpc2.Request) (any, error) {
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

// handleProjectionDetails returns the bits of a projection's
// parsed config the editor needs to drive the Run Projection
// picker. Returns the connection (empty string == none declared)
// and the projection's fixture names. Lookup is by configURI +
// name; missing config or missing projection both surface as
// "config has nothing to say" - the editor falls back to live.
func (s *Server) handleProjectionDetails(req *jsonrpc2.Request) (any, error) {
	params, jerr := decodeParams[ProjectionDetailsParams](req, "projectionDetails")
	if jerr != nil {
		return nil, jerr
	}
	parse, ok := s.docs.GetParse(params.ConfigURI)
	if !ok {
		// No cached parse for this URI - return an empty result
		// rather than an error so the client falls through to
		// "live" without a toast. A truly bogus URI gets the same
		// treatment; the projection lens that produced the call
		// already vouched for the URI's existence.
		return ProjectionDetailsResult{Fixtures: []string{}}, nil
	}
	for _, p := range parse.Description.Projections {
		if p.Name != params.Name {
			continue
		}
		var conn *string
		if parse.Description.Connection != "" {
			c := parse.Description.Connection
			conn = &c
		}
		fixtures := make([]string, 0, len(p.Fixtures))
		for _, fx := range p.Fixtures {
			if fx.Diagnostic == nil {
				fixtures = append(fixtures, fx.Name)
			}
		}
		return ProjectionDetailsResult{
			Connection:   conn,
			Fixtures:     fixtures,
			Environments: parse.Description.Environments,
		}, nil
	}
	return ProjectionDetailsResult{Fixtures: []string{}}, nil
}

// statusLensEnabled reports whether the client opted into the deploy-status
// surface (initializationOptions.statusLens). Guarded by mu, like the other
// initialize-time capability flags.
func (s *Server) statusLensEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLensCapable
}

// handleDidSave refreshes deploy status when a gaffer.toml is saved, so the env
// surface updates without relying on the file watcher (which not every client
// registers). The save may have changed the drift inputs, so recompute drift.
// The buffer is already current from full sync, so there's nothing to reparse.
func (s *Server) handleDidSave(req *jsonrpc2.Request) (any, error) {
	params, jerr := decodeParams[DidSaveTextDocumentParams](req, "didSave")
	if jerr != nil {
		return nil, jerr
	}
	s.refreshStatus(params.TextDocument.URI, true)
	return nil, nil
}

// handleRefreshStatus re-reads deploy status for the named gaffer.toml on the
// editor's request. Params.Poll marks a routine liveness poll (refresh runtime
// only, reusing the cached drift verdict); without it the request is treated as
// change-driven (a sign-in), recomputing drift. The read is async; the fresh
// status reaches the editor through the normal codeLens refresh once it lands.
func (s *Server) handleRefreshStatus(req *jsonrpc2.Request) (any, error) {
	params, jerr := decodeParams[RefreshStatusParams](req, "refreshStatus")
	if jerr != nil {
		return nil, jerr
	}
	s.refreshStatus(params.URI, !params.Poll)
	return nil, nil
}

func (s *Server) handleShutdown() (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shutdownReq = true
	return nil, nil
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
