package lsp

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sourcegraph/jsonrpc2"
)

// Stats is the typed counter snapshot the cobra RunE drains at
// tx.End() time. Lives in lsp (not telemetry) so the server stays
// free of telemetry imports; the translation to typed Tx setters
// happens at the cobra layer.
//
// Counters record attempts, not successes - handlers bump on
// entry, so a codeLens request with nil params or a publish to
// a disconnected client both count. The dataset reflects what
// gaffer chose to do, not what bytes reached the editor.
type Stats struct {
	CodeLensRequestCount   int
	DiagnosticPublishCount int
}

// serverStats holds the in-flight counters mutated by request
// goroutines. Atomics so the request path stays lock-free; the
// main goroutine reads via Stats() at Run-return time.
type serverStats struct {
	codeLensRequests    atomic.Int64
	diagnosticPublishes atomic.Int64
}

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

	// statusCache holds fetched per-env deploy status keyed by config
	// URI, refreshed on open/save/manual request and read by the env
	// status surface. statusFetch does one env's full dial-and-read;
	// runtimeFetch is the cheap poll tier that reuses a cached drift
	// verdict and refreshes only live runtime state. Both are fields so
	// tests inject fakes in place of a live KurrentDB.
	statusCache  *statusCache
	statusFetch  statusFetchFunc
	runtimeFetch statusRuntimeFunc

	// watches holds one live per-env definition-stream subscription per open
	// gaffer.toml (key uri\x00env), so a server-side deploy pushes a drift
	// refresh instead of waiting for a poll. Guarded by watchMu. watchRun does
	// the dial-and-subscribe loop; it's a field so tests inject a fake.
	watchMu        sync.Mutex
	watches        map[string]*watchEntry
	watchRun       envWatchFunc
	watchCapWarned bool

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
	// runCtxFn returns the active run-scope context, or nil after
	// Run's defer clears it. Stored as a closure rather than a
	// `context.Context` field so the type doesn't trip golangci-
	// lint's `containedctx`. Long-running work spawned from
	// handlers - the workspace walk, watched-file event processing
	// - derives its context from this so shutdown doesn't leave
	// goroutines blocked on I/O after the connection is gone.
	runCtxFn  func() context.Context
	cancelRun context.CancelFunc
	// ready gates handler dispatch on Run having stored conn/runCtxFn.
	// jsonrpc2.NewConn starts dispatching the moment it's constructed,
	// so without the gate a fast client's `initialized` can reach
	// spawnWithCtx while runCtxFn is still nil - silently dropping the
	// workspace walk, which nothing retries. Created before the conn,
	// closed once the run state is stored; nil when Run isn't active
	// (handlers can't be dispatched then anyway - no conn exists).
	ready chan struct{}
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
	// statusLensCapable mirrors the client's initializationOptions.statusLens:
	// only a client that opts in (the VS Code extension) fetches and receives
	// the deploy-status lenses, since the informational roll-up isn't a
	// routable command a generic LSP client could render sanely.
	statusLensCapable bool
	// exitCh closes when the client sends `exit`. Run selects on
	// this so the server tears down its connection without waiting
	// for the client to also close stdin (a well-behaved client
	// expects the server to drive disconnect on exit).
	exitCh chan struct{}

	// wg tracks goroutines spawned from handlers (parse-and-
	// publish, the initialized walk, watched-file event batches)
	// so Run can wait for them to drain before returning.
	wg sync.WaitGroup

	stats serverStats
}

// Stats returns the current counter snapshot. Safe to call from
// any goroutine; cobra's RunE calls this after Run has returned
// so the values reflect the full session.
func (s *Server) Stats() Stats {
	return Stats{
		CodeLensRequestCount:   int(s.stats.codeLensRequests.Load()),
		DiagnosticPublishCount: int(s.stats.diagnosticPublishes.Load()),
	}
}

// NewServer constructs a server with the given options. Doesn't
// touch I/O; call Run to start the message loop.
func NewServer(opts ServerOptions) *Server {
	window := opts.DebounceWindow
	if window <= 0 {
		window = defaultDebounceWindow
	}
	s := &Server{
		opts:        opts,
		docs:        newDocumentStore(),
		debouncer:   newDebouncer(window),
		exitCh:      make(chan struct{}),
		statusCache: newStatusCache(),
		watches:     map[string]*watchEntry{},
	}
	s.statusFetch = s.fetchEnvStatus
	s.runtimeFetch = s.fetchEnvRuntime
	s.watchRun = s.runEnvWatch
	return s
}
