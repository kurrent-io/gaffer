package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	mcp     *mcp.Server
	root    string
	cfg     *config.Config
	version string

	// projectOverride pins project resolution to a directory instead
	// of walking up from the cwd (the --project flag / GAFFER_PROJECT
	// env). Empty means cwd-based resolution. Set once at construction,
	// before any handler goroutine, so it needs no lock.
	projectOverride string

	// startedInProject records whether a project was in scope at
	// construction (cfg != nil), for the started_in_project telemetry
	// property. Immutable; lazy resolution doesn't change it.
	startedInProject bool

	mu      sync.Mutex
	session *activeSession

	// projMu guards the shared cfg/root snapshot. root is resolved once
	// (it never moves during a session); cfg is re-read from disk on
	// every project-dependent tool call (see loadProject) so a manifest
	// edit - or a `gaffer init` that creates a previously missing
	// project - is picked up without a restart. Handlers use the
	// cfg/root that loadProject returns, not these fields directly; the
	// snapshot exists for Config() (end-of-session telemetry) and
	// projectRoot() (the config resource).
	projMu sync.Mutex

	stats serverStats

	// projErrMu guards projErrs. Tool-call runners fault on
	// background goroutines (live subscription, fixture loop);
	// the cobra RunE reads the slice at End time after the MCP
	// session has shut down. Mutex is simpler than channel-based
	// coordination for an append-only collection drained once.
	projErrMu sync.Mutex
	projErrs  []error
}

// Config returns the most recently loaded gaffer.toml snapshot. Used by
// the cobra RunE to derive manifest-level telemetry properties at End
// time. With per-call reload this reflects the manifest as of the last
// project-dependent tool call (nil if none ever ran in a project-less
// session).
func (s *Server) Config() *config.Config {
	s.projMu.Lock()
	defer s.projMu.Unlock()
	return s.cfg
}

// StartedInProject reports whether a gaffer project was in scope when
// the server was constructed. False means it started project-less
// (launched outside a project). Drives the started_in_project telemetry.
func (s *Server) StartedInProject() bool {
	return s.startedInProject
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
	return slices.Clone(s.projErrs)
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

	// debug is true when the run requested a breakpoint or break_at, so a
	// timeout can name a breakpoint; live is true for a live subscription,
	// so a timeout can name catching up. Both shape the timeout message.
	debug bool
	live  bool

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

// compileProjection runs the shared engine compile preamble (find,
// config-error, read source, create session) for a named projection and
// records a runtime projection error into projection_errors_seen
// consistently across every tool - the classification that previously
// drifted (events.go recorded on any CreateSession failure; the others
// only on a gafferruntime.ProjectionError). The typed error from
// engine.CompileNamed is returned for the caller to shape into its own
// result (toolError vs the valid:false report); the caller owns
// CompileResult.Session and must Destroy it.
func (s *Server) compileProjection(cfg *config.Config, root, name string, debug bool) (*engine.CompileResult, error) {
	res, err := engine.CompileNamed(cfg, root, name, debug, false)
	if err != nil {
		var projErr gafferruntime.ProjectionError
		if errors.As(err, &projErr) {
			s.recordProjectionError(err)
		}
		return nil, err
	}
	return res, nil
}

func New(root string, cfg *config.Config, version string) *Server {
	s := &Server{
		root:             root,
		cfg:              cfg,
		version:          version,
		startedInProject: cfg != nil,
	}

	s.mcp = mcp.NewServer(
		&mcp.Implementation{
			Name:    "gaffer",
			Version: version,
		},
		&mcp.ServerOptions{
			Instructions: instructionsFor(cfg),
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
	mcp.AddTool(s.mcp, infoTool, trackedTool(s, s.handleInfo))
	mcp.AddTool(s.mcp, initTool, trackedTool(s, s.handleInit))
	mcp.AddTool(s.mcp, versionTool, trackedTool(s, s.handleVersion))
	s.registerResources()
	s.registerPrompts()

	return s
}

// instructionsFor picks the MCP initialize instructions. They're
// fixed for the session (the protocol sends them once), so they
// describe the startup state: a project-less server points the agent
// at the docs resources and how to get a project, since the
// projection tools error until one exists.
func instructionsFor(cfg *config.Config) string {
	if cfg == nil {
		return "Gaffer is a projection toolkit for KurrentDB. No gaffer project is loaded " +
			"in the working directory, so the projection tools (list_projections, run, " +
			"scaffold, debug, ...) are unavailable until one exists. The documentation is " +
			"available now as resources: read projection-api, gotchas, examples, and quirks " +
			"to learn the API. To create a project, call the `init` tool; the projection " +
			"tools then work on the next call, no restart needed."
	}
	return "Gaffer is a projection toolkit for KurrentDB. " +
		"Read the projection-api and gotchas resources before writing projections. " +
		"Workflow: list_projections to see what exists, get_projection_info to inspect a single one, " +
		"scaffold to create new ones, run with fixture events to test, " +
		"get_timeline/get_step to inspect results, debug with break_at to pause and " +
		"evaluate expressions. Each run replaces the previous session."
}

// resolveRoot finds the project root. With an override it walks up from
// that directory (so --project may point at the root or a subdirectory);
// otherwise it walks up from the cwd. Empty means no project found.
func resolveRoot(override string) string {
	if override != "" {
		return project.FindRootFrom(override)
	}
	return project.FindRoot()
}

// NewFromProjectRoot builds the server for `gaffer mcp`. When no
// gaffer.toml is found, it starts project-less (cfg==nil) rather than
// failing, so the server stays launchable from anywhere - the docs
// resources and get_version still work, and project-dependent tools
// resolve the project lazily on first use (see loadProject()). A
// gaffer.toml that exists but fails to parse/validate still surfaces as
// a startup error: that's a real problem the user wants to see, not a
// missing project.
//
// projectOverride (from --project / GAFFER_PROJECT) pins resolution to a
// directory instead of the cwd; empty restores the cwd walk.
func NewFromProjectRoot(version, projectOverride string) (*Server, error) {
	override := normalizeOverride(projectOverride)

	root := resolveRoot(override)
	if root == "" {
		s := New("", nil, version)
		s.projectOverride = override
		return s, nil
	}

	cfg, err := config.Load(project.ConfigPath(root))
	if err != nil {
		return nil, err
	}

	s := New(root, cfg, version)
	s.projectOverride = override
	return s, nil
}

// normalizeOverride makes the override absolute so the resolved root and
// the no-project message are stable regardless of later cwd changes. A
// path that can't be resolved is passed through unchanged.
func normalizeOverride(override string) string {
	if override == "" {
		return ""
	}
	if abs, err := filepath.Abs(override); err == nil {
		return abs
	}
	return override
}

// loadProject resolves the project root and re-reads gaffer.toml from
// disk on every call, so a manifest edit - or a `gaffer init` that
// creates a previously missing project - is visible to the next tool
// call without a restart. It returns the freshly parsed config and root,
// (nil, "", nil) when no project is in scope, or a non-nil error when a
// gaffer.toml exists but fails to load or validate.
//
// The root is resolved once and cached in s.root (it never moves during
// a session); only the config content is re-read. The fresh cfg is
// snapshotted into s.cfg under projMu for Config()'s end-of-session
// telemetry. Callers use the returned values, not the shared fields, so
// a concurrent reload can't race their reads.
func (s *Server) loadProject() (*config.Config, string, error) {
	root := s.projectRoot()
	if root == "" {
		return nil, "", nil
	}
	cfg, err := config.Load(project.ConfigPath(root))
	if err != nil {
		return nil, "", err
	}
	s.projMu.Lock()
	s.root, s.cfg = root, cfg
	s.projMu.Unlock()
	return cfg, root, nil
}

// projectRoot returns the root the server already knows (set at
// construction or by a prior successful resolve), else one found by
// walking up from the cwd. Unlike project(), it does not parse or
// validate gaffer.toml - the config resource uses it so it can surface
// the manifest even when the manifest fails to load.
func (s *Server) projectRoot() string {
	s.projMu.Lock()
	root := s.root
	s.projMu.Unlock()
	if root != "" {
		return root
	}
	return resolveRoot(s.projectOverride)
}

// requireProject is the tool-handler gate for project-dependent tools.
// It re-reads gaffer.toml (see loadProject) and returns the parsed config
// with the project root, or a tool-error result when no project is in
// scope or the manifest fails to load. Handlers use the returned cfg/root
// rather than s.cfg/s.root so each call sees the manifest as it is on
// disk now. Mirrors requireSession.
func (s *Server) requireProject() (*config.Config, string, *mcp.CallToolResult) {
	cfg, root, err := s.loadProject()
	if err != nil {
		return nil, "", toolError("loading gaffer.toml: %v", err)
	}
	if cfg == nil {
		return nil, "", toolError("%s", s.noProjectMessage())
	}
	return cfg, root, nil
}

// noProjectMessage explains that no project was found and how to point
// the server at one. It names the actual search origin - the --project /
// GAFFER_PROJECT override when set, otherwise the cwd - so the user can
// see where gaffer looked.
func (s *Server) noProjectMessage() string {
	if s.projectOverride != "" {
		return fmt.Sprintf("no gaffer project found under %s (from --project / GAFFER_PROJECT). "+
			"Call the `init` tool to create one there, or point it at a directory containing gaffer.toml.", s.projectOverride)
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "the working directory"
	}
	return fmt.Sprintf("no gaffer project found (searched upward from %s). "+
		"Call the `init` tool to create one, restart gaffer mcp from a directory containing gaffer.toml, "+
		"or pass --project / set GAFFER_PROJECT.", cwd)
}

func (s *Server) Run(ctx context.Context) error {
	err := s.mcp.Run(ctx, &mcp.StdioTransport{})
	s.mu.Lock()
	s.closeSession()
	s.mu.Unlock()
	return err
}

func (s *Server) connectToKurrentDB(cfg *config.Config, root, envName string) (*kurrentdb.Client, error) {
	env, err := mcpConnection(cfg, envName)
	if err != nil {
		return nil, err
	}
	// The auth-invalidation handle drives the editor's re-sign-in prompt on a
	// debug run; the MCP server has no such UX, so it's dropped. A rejected
	// token is still cleared by the provider, so the next call self-heals.
	client, _, err := engine.Connect(env.Connection, root, env.Name, env.OAuth, env.Cert)
	return client, err
}

// mcpConnection resolves the env the mcp server dials. envName selects a
// named [env.<name>]; an empty string uses the env marked default. A
// project with no matching env (or no default when none is named) can't
// be reached over the server-touching tools. The returned name drives
// the .env.<env> overlay at connect time.
func mcpConnection(cfg *config.Config, envName string) (config.ResolvedEnv, error) {
	return cfg.ResolveEnv(envName)
}

// closeSession tears down the active session. The caller must hold s.mu,
// which stays held for the entire teardown.
//
// s.session is captured into a local and cleared up front, so a concurrent
// teardown-triggering call (stop+stop, stop+run, run+run) that acquires
// s.mu next sees no session and can't double-Destroy or nil-deref. The
// background feed/live goroutine never takes s.mu, so waiting on its done
// channel while holding the lock can't deadlock. Handlers parked in
// waitForBreak re-check session identity after re-acquiring s.mu (see
// handleWaitResult), so they won't touch the runner destroyed here.
func (s *Server) closeSession() {
	sess := s.session
	if sess == nil {
		return
	}
	s.session = nil

	if sess.cancel != nil {
		sess.cancel()
	}
	// Force a paused/break_at session to run to completion before waiting
	// for the feed goroutine, so <-done can't block forever (Drain is
	// terminal, unlike a bare ClearBreakpoints+Continue which races the
	// async step a break_at pause converts into).
	sess.runner.Drain()
	if sess.done != nil {
		<-sess.done
	}
	sess.runner.Destroy()
}

func (s *Server) createSession(cfg *config.Config, root, name string, debug bool) (*activeSession, error) {
	s.closeSession()

	compiled, err := s.compileProjection(cfg, root, name, debug)
	if err != nil {
		return nil, err
	}
	lp := compiled.Projection
	runtime, info := compiled.Session, compiled.Info

	store, err := history.New()
	if err != nil {
		runtime.Destroy()
		return nil, fmt.Errorf("creating history store: %w", err)
	}

	sess := &activeSession{debug: debug}

	runnerCfg := engine.RunnerConfig{
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

		runnerCfg.Debug = &engine.DebugConfig{
			Session: runtime,
			Info:    info,
			OnBreak: func(bi gafferruntime.BreakInfo) {
				select {
				case breakCh <- bi:
				default:
				}
			},
			// The runner's internal auto-step (break_at) runs on its own
			// goroutine; route its errors to errorCh so waitForBreak reports
			// them instead of timing out with a misleading breakpoint message.
			// Non-blocking send, matching OnBreak/the feed-error path: errorCh
			// is buffered 1 and waitForBreak reads one value per wait, so if a
			// feed error already holds the slot this drop is harmless.
			OnError: func(err error) {
				select {
				case sess.errorCh <- err:
				default:
				}
			},
		}
	}
	sess.runner = engine.NewRunner(runnerCfg)

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
//
// It also recovers any panic the handler raises and reports it as a
// tool error. The go-sdk dispatches tool calls concurrently on their
// own goroutines and does not recover handler panics, so an unrecovered
// panic (e.g. a use-after-free against a session a concurrent teardown
// destroyed) would take the whole gaffer mcp process down. This mirrors
// the recover guards on the DAP path.
func trackedTool[In, Out any](
	s *Server,
	fn func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error),
) func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (res *mcp.CallToolResult, out Out, err error) {
		s.stats.toolCalls.Add(1)
		defer func() {
			if r := recover(); r != nil {
				res, out, err = toolError("internal error: %v", r), *new(Out), nil
			}
		}()
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
