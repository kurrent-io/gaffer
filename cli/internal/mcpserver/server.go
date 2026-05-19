package mcpserver

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/history"
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Stats is the typed counter snapshot the cobra RunE drains at
// tx.End() time. Lives in mcpserver (not telemetry) so the server
// stays free of any telemetry imports - the translation to typed
// Tx setters happens at the cobra layer.
//
// Counters record attempts, not successes - the tracked wrappers
// bump on dispatch entry, so failed tool calls (`tool_error`
// results) and resource reads against missing files both count.
// That's the right semantic: "the user asked for this" is usage
// signal regardless of how gaffer answered.
type Stats struct {
	ToolCallCount     int
	ResourceReadCount int
}

// serverStats holds the in-flight counters mutated by tool /
// resource dispatch on request goroutines. Atomics so the request
// path stays lock-free; the main goroutine reads via Stats() at
// shutdown which sees the final value via the release fence.
type serverStats struct {
	toolCalls     atomic.Int64
	resourceReads atomic.Int64
}

type Server struct {
	mcp  *mcp.Server
	root string
	cfg  *config.Config

	mu      sync.Mutex
	session *activeSession

	stats serverStats

	// projErrMu guards projErrs. Tool-call runners fault on
	// background goroutines (live subscription, fixture loop);
	// the cobra RunE reads the slice at End time after the MCP
	// session has shut down. Mutex is simpler than channel-based
	// coordination for an append-only collection drained once.
	projErrMu sync.Mutex
	projErrs  []error
}

// Config returns the parsed gaffer.toml the server was constructed
// with. Used by the cobra RunE to derive manifest-level telemetry
// properties at End time without re-loading the file.
func (s *Server) Config() *config.Config {
	return s.cfg
}

// Stats returns the current counter snapshot. Safe to call from
// any goroutine; the cobra RunE for `gaffer mcp` calls this at
// tx.End time after the underlying mcp.Server.Run has returned.
func (s *Server) Stats() Stats {
	return Stats{
		ToolCallCount:     int(s.stats.toolCalls.Load()),
		ResourceReadCount: int(s.stats.resourceReads.Load()),
	}
}

// ProjectionErrors returns the raw FFI errors captured across every
// run-tool invocation in this session, in observation order. The
// cobra wrapper classifies them at End time so the schema-specific
// outcome mapping stays out of the server. Returns a copy; safe to
// hold past further fault events.
func (s *Server) ProjectionErrors() []error {
	s.projErrMu.Lock()
	defer s.projErrMu.Unlock()
	if len(s.projErrs) == 0 {
		return nil
	}
	out := make([]error, len(s.projErrs))
	copy(out, s.projErrs)
	return out
}

// recordProjectionError stashes a runner fault so the cobra wrapper
// can drain it into projection_errors_seen. Called from the run-tool
// background goroutines and from the live-mode subscription loop;
// the mutex serialises the slice append across them.
func (s *Server) recordProjectionError(err error) {
	if err == nil {
		return
	}
	s.projErrMu.Lock()
	s.projErrs = append(s.projErrs, err)
	s.projErrMu.Unlock()
}

type activeSession struct {
	runner *engine.Runner
	cancel context.CancelFunc

	// MCP coordination channels
	breakCh    chan gafferruntime.BreakInfo
	done       chan struct{} // closed when background feed goroutine exits
	caughtUpCh chan struct{} // signaled when live subscription catches up
	errorCh    chan error    // signaled on feed error in background
}

func (sess *activeSession) handled() int64 {
	return int64(sess.runner.Stats().Handled)
}

func (sess *activeSession) errors() int64 {
	return int64(sess.runner.Stats().Errors)
}

func (s *Server) requireSession() (*activeSession, *mcp.CallToolResult) {
	if s.session == nil {
		return nil, toolError("no active session - call run first")
	}
	return s.session, nil
}

func New(root string, cfg *config.Config, version string) *Server {
	s := &Server{
		root: root,
		cfg:  cfg,
	}

	s.mcp = mcp.NewServer(
		&mcp.Implementation{
			Name:    "gaffer",
			Version: version,
		},
		&mcp.ServerOptions{
			Instructions: "Gaffer is a projection toolkit for KurrentDB. " +
				"Read the projection-api and gotchas resources before writing projections. " +
				"Workflow: list_projections to see what exists, scaffold to create new ones, " +
				"run with fixture events to test, get_timeline/get_step to inspect results, " +
				"debug with break_at to pause and evaluate expressions. " +
				"Each run replaces the previous session.",
		},
	)

	mcp.AddTool(s.mcp, validateTool, trackedTool(s, s.handleValidate))
	mcp.AddTool(s.mcp, runTool, trackedTool(s, s.handleRun))
	mcp.AddTool(s.mcp, stopTool, trackedTool(s, s.handleStop))
	mcp.AddTool(s.mcp, getStepTool, trackedTool(s, s.handleGetStep))
	mcp.AddTool(s.mcp, getHistoryTool, trackedTool(s, s.handleGetHistory))
	mcp.AddTool(s.mcp, getTimelineTool, trackedTool(s, s.handleGetTimeline))
	mcp.AddTool(s.mcp, getStateTool, trackedTool(s, s.handleGetState))
	mcp.AddTool(s.mcp, listProjectionsTool, trackedTool(s, s.handleListProjections))
	mcp.AddTool(s.mcp, scaffoldTool, trackedTool(s, s.handleScaffold))
	mcp.AddTool(s.mcp, evaluateTool, trackedTool(s, s.handleEvaluate))
	mcp.AddTool(s.mcp, debugContinueTool, trackedTool(s, s.handleDebugContinue))
	mcp.AddTool(s.mcp, stepOverTool, trackedTool(s, s.handleStepOver))
	mcp.AddTool(s.mcp, stepIntoTool, trackedTool(s, s.handleStepInto))
	mcp.AddTool(s.mcp, stepOutTool, trackedTool(s, s.handleStepOut))
	mcp.AddTool(s.mcp, listEventsTool, trackedTool(s, s.handleListEvents))
	s.registerResources()
	s.registerPrompts()

	return s
}

func NewFromProjectRoot(version string) (*Server, error) {
	root := project.FindRoot()
	if root == "" {
		return nil, project.ErrNotInProject
	}

	cfg, err := config.Load(project.ConfigPath(root))
	if err != nil {
		return nil, err
	}

	return New(root, cfg, version), nil
}

func (s *Server) Run(ctx context.Context) error {
	err := s.mcp.Run(ctx, &mcp.StdioTransport{})
	s.mu.Lock()
	s.closeSession()
	s.mu.Unlock()
	return err
}

func (s *Server) connectToKurrentDB() (*kurrentdb.Client, error) {
	if s.cfg.Connection == "" {
		return nil, fmt.Errorf("no connection configured in gaffer.toml")
	}
	return engine.Connect(s.cfg.Connection, s.root)
}

func (s *Server) closeSession() {
	if s.session != nil {
		if s.session.cancel != nil {
			s.session.cancel()
		}
		// Unblock any Feed paused at a breakpoint before waiting for goroutine
		s.session.runner.ClearBreakpoints()
		if s.session.runner.Paused() {
			s.session.runner.Continue()
		}
		if s.session.done != nil {
			done := s.session.done
			s.mu.Unlock()
			<-done
			s.mu.Lock()
		}
		s.session.runner.Destroy()
		s.session = nil
	}
}

func (s *Server) createSession(name string, debug bool) (*activeSession, error) {
	s.closeSession()

	proj := s.cfg.FindProjection(name)
	if proj == nil {
		return nil, fmt.Errorf("projection %q not found in gaffer.toml", name)
	}

	source, err := engine.ReadSource(s.root, proj.Entry)
	if err != nil {
		return nil, err
	}

	lp := engine.NewProjection(s.root, s.cfg, proj, source)
	runtime, info, err := engine.CreateSession(lp, debug, false)
	if err != nil {
		return nil, err
	}

	store, err := history.New()
	if err != nil {
		runtime.Destroy()
		return nil, fmt.Errorf("creating history store: %w", err)
	}

	sess := &activeSession{}

	cfg := engine.RunnerConfig{
		Feed:          engine.FeedFn(runtime.Feed),
		Session:       runtime,
		Info:          info,
		EngineVersion: lp.EngineVersion,
		Writer:        nil,
		History:       store,
	}
	if debug {
		breakCh := make(chan gafferruntime.BreakInfo, 1)
		sess.breakCh = breakCh
		sess.caughtUpCh = make(chan struct{}, 1)
		sess.errorCh = make(chan error, 1)

		cfg.Debug = &engine.DebugConfig{
			Session: runtime,
			Info:    info,
			OnBreak: func(bi gafferruntime.BreakInfo) {
				select {
				case breakCh <- bi:
				default:
				}
			},
		}
	}
	sess.runner = engine.NewRunner(cfg)

	if debug {
		sess.runner.SetStatus("debugging")
	} else {
		sess.runner.SetStatus("ready")
	}

	s.session = sess
	return sess, nil
}

// trackedTool wraps a typed tool handler with a tool-call counter
// bump so the protocol-handler hot path stays free of telemetry
// concerns. Package-level (rather than a method on *Server)
// because Go disallows type parameters on methods - In/Out vary
// per tool.
func trackedTool[In, Out any](
	s *Server,
	fn func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error),
) func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		s.stats.toolCalls.Add(1)
		return fn(ctx, req, in)
	}
}

// trackedResource wraps a resource handler with a read-count
// bump. mcp.ResourceHandler is a concrete func type so this stays
// a regular method.
func (s *Server) trackedResource(handler mcp.ResourceHandler) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		s.stats.resourceReads.Add(1)
		return handler(ctx, req)
	}
}
