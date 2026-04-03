package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/history"
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Server struct {
	mcp  *mcp.Server
	root string
	cfg  *config.Config

	mu      sync.Mutex
	session *activeSession
}

type activeSession struct {
	runtime    *gafferruntime.Session
	history    *history.Store
	info       gafferruntime.QuerySources
	name       string
	runner     *engine.Runner
	stats      sessionStats
	partitions map[string]bool
	cancel     context.CancelFunc
	lastError  error

	// Debug state (pausedEvent/breakAtPosition used by live debug path until migrated)
	pausedEvent     string
	breakCh         chan gafferruntime.BreakInfo
	done            chan struct{} // closed when background feed goroutine exits
	caughtUpCh      chan struct{} // signaled when live subscription catches up
	errorCh         chan error    // signaled on feed error in background
	breakAtPosition int64         // pause at this event position (1-based), 0 = disabled
}

type sessionStats struct {
	Processed int64  `json:"processed"`
	Skipped   int64  `json:"skipped"`
	Errors    int64  `json:"errors"`
	Status    string `json:"status"`
}

func (sess *activeSession) handled() int64 {
	if sess.runner != nil {
		return int64(sess.runner.Stats().Handled)
	}
	return sess.stats.Processed
}

func (sess *activeSession) skipped() int64 {
	if sess.runner != nil {
		return int64(sess.runner.Stats().Skipped)
	}
	return sess.stats.Skipped
}

func (sess *activeSession) errors() int64 {
	if sess.runner != nil {
		return int64(sess.runner.Stats().Errors)
	}
	return sess.stats.Errors
}

func (sess *activeSession) eventCount() int64 {
	return sess.handled() + sess.skipped() + sess.errors()
}

func (sess *activeSession) activePartitions() map[string]bool {
	if sess.runner != nil {
		return sess.runner.Partitions()
	}
	return sess.partitions
}

func New(root string, cfg *config.Config) *Server {
	s := &Server{
		root: root,
		cfg:  cfg,
	}

	s.mcp = mcp.NewServer(
		&mcp.Implementation{
			Name:    "gaffer",
			Version: "0.1.0",
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

	s.registerTools()
	s.registerResources()
	s.registerPrompts()

	return s
}

func NewFromProjectRoot() (*Server, error) {
	root := project.FindRoot()
	if root == "" {
		return nil, fmt.Errorf("not in a gaffer project (no gaffer.toml found)")
	}

	cfg, err := config.Load(filepath.Join(root, "gaffer.toml"))
	if err != nil {
		return nil, err
	}

	return New(root, cfg), nil
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
		s.session.runner.Destroy()
		if s.session.done != nil {
			done := s.session.done
			s.mu.Unlock()
			<-done
			s.mu.Lock()
		}
		s.session.runtime.Destroy()
		_ = s.session.history.Close()
		s.session = nil
	}
}

func (s *Server) createSession(name string, debug bool) (*activeSession, error) {
	s.closeSession()

	proj := s.cfg.FindProjection(name)
	if proj == nil {
		return nil, fmt.Errorf("projection %q not found in gaffer.toml", name)
	}

	source, err := os.ReadFile(filepath.Join(s.root, proj.Entry))
	if err != nil {
		return nil, fmt.Errorf("reading projection source: %w", err)
	}

	lp := engine.NewLoadedProjection(s.root, s.cfg, proj, string(source))
	runtime, info, err := engine.NewSession(lp, debug)
	if err != nil {
		return nil, err
	}

	store, err := history.New()
	if err != nil {
		runtime.Destroy()
		return nil, fmt.Errorf("creating history store: %w", err)
	}

	status := "ready"
	if debug {
		status = "debugging"
	}

	sess := &activeSession{
		runtime:    runtime,
		history:    store,
		info:       info,
		name:       name,
		partitions: make(map[string]bool),
		stats:      sessionStats{Status: status},
	}

	cfg := engine.RunnerConfig{
		Feed:    engine.FeedFn(runtime.Feed),
		Writer:  nil,
		History: store,
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

	s.session = sess
	return sess, nil
}
